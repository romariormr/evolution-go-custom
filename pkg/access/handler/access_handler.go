package access_handler

import (
	"net/http"
	"strings"

	access_model "github.com/EvolutionAPI/evolution-go/pkg/access/model"
	access_service "github.com/EvolutionAPI/evolution-go/pkg/access/service"
	instance_service "github.com/EvolutionAPI/evolution-go/pkg/instance/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const sessionCookie = "evogo_session"

type AccessHandler struct {
	service         access_service.AccessService
	instanceService instance_service.InstanceService
}

func NewAccessHandler(service access_service.AccessService, instanceService instance_service.InstanceService) *AccessHandler {
	return &AccessHandler{service: service, instanceService: instanceService}
}

// RegisterRoutes monta o grupo /access no engine.
func RegisterRoutes(eng *gin.Engine, h *AccessHandler) {
	pub := eng.Group("/access")
	{
		pub.POST("/login", h.Login)
		pub.POST("/logout", h.Logout)
		pub.GET("/branding", h.Branding)
	}

	auth := eng.Group("/access")
	auth.Use(h.SessionMiddleware)
	{
		auth.GET("/me", h.Me)
		auth.POST("/me/password", h.ChangePassword)
		auth.GET("/instances", h.ListInstances)
		auth.POST("/instances", h.CreateInstance)
		auth.DELETE("/instances/:instanceId", h.DeleteInstance)
	}

	admin := eng.Group("/access/admin")
	admin.Use(h.SessionMiddleware, h.AdminOnly)
	{
		admin.GET("/users", h.AdminListUsers)
		admin.POST("/users", h.AdminCreateUser)
		admin.PUT("/users/:userId/groups", h.AdminSetUserGroups)
		admin.PUT("/users/:userId/password", h.AdminResetPassword)
		admin.DELETE("/users/:userId", h.AdminDeleteUser)

		admin.GET("/groups", h.AdminListGroups)
		admin.POST("/groups", h.AdminCreateGroup)
		admin.DELETE("/groups/:groupId", h.AdminDeleteGroup)
		admin.POST("/groups/:groupId/instances/:instanceId", h.AdminLinkInstance)
		admin.DELETE("/groups/:groupId/instances/:instanceId", h.AdminUnlinkInstance)

		admin.GET("/settings", h.AdminListSettings)
		admin.PUT("/settings/:key", h.AdminSetSetting)
		admin.POST("/settings/ldap/test", h.AdminTestLdap)
	}
}

// ── middlewares ──────────────────────────────────────────────────

func (h *AccessHandler) SessionMiddleware(ctx *gin.Context) {
	token := ""
	if c, err := ctx.Cookie(sessionCookie); err == nil {
		token = c
	}
	if token == "" {
		authz := ctx.GetHeader("Authorization")
		if strings.HasPrefix(authz, "Bearer ") {
			token = strings.TrimPrefix(authz, "Bearer ")
		}
	}
	if token == "" {
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "não autenticado"})
		return
	}
	user, err := h.service.ValidateSession(token)
	if err != nil {
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "sessão inválida ou expirada"})
		return
	}
	ctx.Set("accessUser", user)
	ctx.Next()
}

func (h *AccessHandler) AdminOnly(ctx *gin.Context) {
	user := currentUser(ctx)
	if user == nil || user.Role != access_model.RoleAdmin {
		ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "apenas administradores"})
		return
	}
	ctx.Next()
}

func currentUser(ctx *gin.Context) *access_model.AccessUser {
	v, ok := ctx.Get("accessUser")
	if !ok {
		return nil
	}
	u, _ := v.(*access_model.AccessUser)
	return u
}

// ── branding (público) ────────────────────────────────────────────

// brandingKeys: única lista de chaves de evogo_settings expostas sem
// autenticação. NUNCA usar service.ListSettings() puro aqui — ele
// devolveria ldap.bind_password e outros segredos de configuração.
var brandingKeys = map[string]string{
	"branding.app_name": "appName",
	"branding.logo":     "logo",
}

const defaultAppName = "Evolution GO"

func (h *AccessHandler) Branding(ctx *gin.Context) {
	all, err := h.service.ListSettings()
	out := gin.H{"appName": defaultAppName, "logo": ""}
	if err == nil {
		for settingKey, outKey := range brandingKeys {
			if v, ok := all[settingKey]; ok && v != "" {
				out[outKey] = v
			}
		}
	}
	ctx.JSON(http.StatusOK, out)
}

// ── auth ─────────────────────────────────────────────────────────

type loginBody struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (h *AccessHandler) Login(ctx *gin.Context) {
	var body loginBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "username e password são obrigatórios"})
		return
	}
	token, user, err := h.service.Login(body.Username, body.Password)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "usuário ou senha inválidos"})
		return
	}
	ctx.SetSameSite(http.SameSiteLaxMode)
	ctx.SetCookie(sessionCookie, token, int(12*60*60), "/", "", false, true)
	ctx.JSON(http.StatusOK, gin.H{
		"token": token,
		"user":  user,
	})
}

func (h *AccessHandler) Logout(ctx *gin.Context) {
	ctx.SetCookie(sessionCookie, "", -1, "/", "", false, true)
	ctx.JSON(http.StatusOK, gin.H{"message": "logout"})
}

func (h *AccessHandler) Me(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, currentUser(ctx))
}

type changePasswordBody struct {
	CurrentPassword string `json:"currentPassword" binding:"required"`
	NewPassword     string `json:"newPassword" binding:"required"`
}

func (h *AccessHandler) ChangePassword(ctx *gin.Context) {
	var body changePasswordBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "currentPassword e newPassword são obrigatórios"})
		return
	}
	if err := h.service.ChangePassword(currentUser(ctx).Id, body.CurrentPassword, body.NewPassword); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "senha alterada"})
}

// ── instâncias (escopo por grupo) ────────────────────────────────

func (h *AccessHandler) ListInstances(ctx *gin.Context) {
	user := currentUser(ctx)
	allowed, isAdmin, err := h.service.AllowedInstanceIds(user)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	all, err := h.instanceService.GetAll()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if isAdmin {
		ctx.JSON(http.StatusOK, gin.H{"data": all})
		return
	}
	filtered := make([]interface{}, 0, len(all))
	for _, inst := range all {
		if allowed[inst.Id] {
			filtered = append(filtered, inst)
		}
	}
	ctx.JSON(http.StatusOK, gin.H{"data": filtered})
}

type createInstanceBody struct {
	Name    string `json:"name" binding:"required"`
	Token   string `json:"token"`
	GroupId string `json:"groupId"`
}

func (h *AccessHandler) CreateInstance(ctx *gin.Context) {
	user := currentUser(ctx)
	var body createInstanceBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "name é obrigatório"})
		return
	}
	if body.Token == "" {
		body.Token = uuid.NewString()
	}
	inst, err := h.instanceService.Create(&instance_service.CreateStruct{
		Name:  body.Name,
		Token: body.Token,
	})
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.service.LinkInstanceToUserGroups(user, inst.Id, body.GroupId); err != nil {
		// instância criada mas sem vínculo — desfaz pra não virar órfã invisível pro user
		_ = h.instanceService.Delete(inst.Id)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusCreated, gin.H{"data": inst})
}

func (h *AccessHandler) DeleteInstance(ctx *gin.Context) {
	user := currentUser(ctx)
	instanceId := ctx.Param("instanceId")
	allowed, isAdmin, err := h.service.AllowedInstanceIds(user)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !isAdmin && !allowed[instanceId] {
		ctx.JSON(http.StatusForbidden, gin.H{"error": "instância não pertence aos seus grupos"})
		return
	}
	if err := h.instanceService.Delete(instanceId); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	_ = h.service.UnlinkInstanceEverywhere(instanceId)
	ctx.JSON(http.StatusOK, gin.H{"message": "instância removida"})
}

// ── admin: users ─────────────────────────────────────────────────

func (h *AccessHandler) AdminListUsers(ctx *gin.Context) {
	users, err := h.service.ListUsers()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"data": users})
}

type adminCreateUserBody struct {
	Username    string   `json:"username" binding:"required"`
	Password    string   `json:"password" binding:"required"`
	DisplayName string   `json:"displayName"`
	Role        string   `json:"role"`
	GroupIds    []string `json:"groupIds"`
}

func (h *AccessHandler) AdminCreateUser(ctx *gin.Context) {
	var body adminCreateUserBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "username e password são obrigatórios"})
		return
	}
	user, err := h.service.CreateUser(body.Username, body.Password, body.DisplayName, body.Role, body.GroupIds)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusCreated, gin.H{"data": user})
}

type groupIdsBody struct {
	GroupIds []string `json:"groupIds"`
}

func (h *AccessHandler) AdminSetUserGroups(ctx *gin.Context) {
	var body groupIdsBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "groupIds é obrigatório"})
		return
	}
	if err := h.service.UpdateUserGroups(ctx.Param("userId"), body.GroupIds); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "grupos atualizados"})
}

type passwordBody struct {
	Password string `json:"password" binding:"required"`
}

func (h *AccessHandler) AdminResetPassword(ctx *gin.Context) {
	var body passwordBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "password é obrigatório"})
		return
	}
	if err := h.service.ResetPassword(ctx.Param("userId"), body.Password); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "senha redefinida (troca obrigatória no próximo login)"})
}

func (h *AccessHandler) AdminDeleteUser(ctx *gin.Context) {
	if ctx.Param("userId") == currentUser(ctx).Id {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "não é possível excluir o próprio usuário"})
		return
	}
	if err := h.service.DeleteUser(ctx.Param("userId")); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "usuário removido"})
}

// ── admin: groups ────────────────────────────────────────────────

func (h *AccessHandler) AdminListGroups(ctx *gin.Context) {
	groups, err := h.service.ListGroups()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"data": groups})
}

type adminCreateGroupBody struct {
	Name        string `json:"name" binding:"required"`
	LdapGroupDN string `json:"ldapGroupDn"`
}

func (h *AccessHandler) AdminCreateGroup(ctx *gin.Context) {
	var body adminCreateGroupBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "name é obrigatório"})
		return
	}
	group, err := h.service.CreateGroup(body.Name, body.LdapGroupDN)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusCreated, gin.H{"data": group})
}

func (h *AccessHandler) AdminDeleteGroup(ctx *gin.Context) {
	if err := h.service.DeleteGroup(ctx.Param("groupId")); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "grupo removido"})
}

func (h *AccessHandler) AdminLinkInstance(ctx *gin.Context) {
	if err := h.service.LinkInstance(ctx.Param("groupId"), ctx.Param("instanceId")); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "instância vinculada ao grupo"})
}

func (h *AccessHandler) AdminUnlinkInstance(ctx *gin.Context) {
	if err := h.service.UnlinkInstance(ctx.Param("groupId"), ctx.Param("instanceId")); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "vínculo removido"})
}

// ── admin: settings ──────────────────────────────────────────────

func (h *AccessHandler) AdminListSettings(ctx *gin.Context) {
	settings, err := h.service.ListSettings()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"data": settings})
}

type settingBody struct {
	Value string `json:"value"`
}

func (h *AccessHandler) AdminSetSetting(ctx *gin.Context) {
	var body settingBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "value é obrigatório"})
		return
	}
	if err := h.service.SetSetting(ctx.Param("key"), body.Value); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "configuração salva"})
}

func (h *AccessHandler) AdminTestLdap(ctx *gin.Context) {
	if err := h.service.TestLdap(); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "conexão LDAP bem-sucedida"})
}
