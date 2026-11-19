package access_handler

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	access_model "github.com/EvolutionAPI/evolution-go/pkg/access/model"
	access_repository "github.com/EvolutionAPI/evolution-go/pkg/access/repository"
	access_service "github.com/EvolutionAPI/evolution-go/pkg/access/service"
	instance_service "github.com/EvolutionAPI/evolution-go/pkg/instance/service"
	storage_interfaces "github.com/EvolutionAPI/evolution-go/pkg/storage/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	qrcode "github.com/skip2/go-qrcode"
)

const sessionCookie = "evogo_session"

type AccessHandler struct {
	service         access_service.AccessService
	instanceService instance_service.InstanceService
	mediaStorage    storage_interfaces.MediaStorage // nil se MINIO_ENABLED=false
	logoBaseURL     string                          // legado (desde 0.17.3 a logo é servida em /access/branding/logo/:file)
}

func NewAccessHandler(service access_service.AccessService, instanceService instance_service.InstanceService, mediaStorage storage_interfaces.MediaStorage, logoBaseURL string) *AccessHandler {
	return &AccessHandler{service: service, instanceService: instanceService, mediaStorage: mediaStorage, logoBaseURL: logoBaseURL}
}

// RegisterRoutes monta o grupo /access no engine.
func RegisterRoutes(eng *gin.Engine, h *AccessHandler) {
	pub := eng.Group("/access")
	{
		pub.POST("/login", h.Login)
		pub.POST("/login/2fa", h.LoginTwoFactor)
		pub.POST("/logout", h.Logout)
		pub.GET("/branding", h.Branding)
		pub.GET("/branding/logo/:file", h.BrandingLogo)
	}

	auth := eng.Group("/access")
	auth.Use(h.SessionMiddleware)
	{
		auth.GET("/me", h.Me)
		auth.POST("/me/password", h.ChangePassword)
		auth.GET("/me/sessions", h.MySessions)
		auth.DELETE("/me/sessions/:sessionId", h.RevokeMySession)
		auth.POST("/me/2fa/setup", h.TotpSetup)
		auth.POST("/me/2fa/enable", h.TotpEnable)
		auth.POST("/me/2fa/disable", h.TotpDisable)
		auth.GET("/instances", h.ListInstances)
		auth.POST("/instances", h.CreateInstance)
		auth.DELETE("/instances/:instanceId", h.DeleteInstance)
	}

	admin := eng.Group("/access/admin")
	admin.Use(h.SessionMiddleware, h.AdminOnly)
	{
		admin.GET("/users", h.AdminListUsers)
		admin.POST("/users", h.AdminCreateUser)
		admin.PUT("/users/:userId", h.AdminUpdateUser)
		admin.PUT("/users/:userId/groups", h.AdminSetUserGroups)
		admin.PUT("/users/:userId/password", h.AdminResetPassword)
		admin.POST("/users/:userId/2fa/reset", h.AdminResetTotp)
		admin.GET("/users/:userId/sessions", h.AdminUserSessions)
		admin.DELETE("/users/:userId", h.AdminDeleteUser)
		admin.DELETE("/sessions/:sessionId", h.AdminRevokeSession)

		admin.GET("/groups", h.AdminListGroups)
		admin.POST("/groups", h.AdminCreateGroup)
		admin.DELETE("/groups/:groupId", h.AdminDeleteGroup)
		admin.POST("/groups/:groupId/api-key/regenerate", h.AdminRegenerateGroupApiKey)
		admin.POST("/groups/:groupId/instances/:instanceId", h.AdminLinkInstance)
		admin.DELETE("/groups/:groupId/instances/:instanceId", h.AdminUnlinkInstance)
		admin.GET("/instances/:instanceId/groups", h.AdminInstanceGroups)

		admin.GET("/audit", h.AdminAudit)

		admin.GET("/settings", h.AdminListSettings)
		admin.PUT("/settings/:key", h.AdminSetSetting)
		admin.POST("/settings/ldap/test", h.AdminTestLdap)
		admin.POST("/settings/ldap/test-user", h.AdminTestLdapUser)
		admin.POST("/branding/logo", h.AdminUploadLogo)
	}
}

// ── middlewares ──────────────────────────────────────────────────

func sessionToken(ctx *gin.Context) string {
	if authz := ctx.GetHeader("Authorization"); strings.HasPrefix(authz, "Bearer ") {
		return strings.TrimPrefix(authz, "Bearer ")
	}
	if c, err := ctx.Cookie(sessionCookie); err == nil {
		return c
	}
	return ""
}

func clientInfo(ctx *gin.Context) access_service.ClientInfo {
	return access_service.ClientInfo{IP: ctx.ClientIP(), UserAgent: ctx.Request.UserAgent()}
}

func isHTTPS(ctx *gin.Context) bool {
	return ctx.Request.TLS != nil || strings.EqualFold(ctx.GetHeader("X-Forwarded-Proto"), "https")
}

func (h *AccessHandler) SessionMiddleware(ctx *gin.Context) {
	token := sessionToken(ctx)
	if token == "" {
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "não autenticado"})
		return
	}
	user, sess, err := h.service.ValidateSession(token)
	if err != nil {
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "sessão inválida ou expirada"})
		return
	}
	ctx.Set("accessUser", user)
	ctx.Set("accessSession", sess)
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

func currentSessionId(ctx *gin.Context) string {
	if v, ok := ctx.Get("accessSession"); ok {
		if s, ok := v.(*access_model.AccessSession); ok && s != nil {
			return s.Id
		}
	}
	return ""
}

func (h *AccessHandler) audit(ctx *gin.Context, action, target, detail string, success bool) {
	h.service.Audit(currentUser(ctx), clientInfo(ctx), action, target, detail, success)
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
	// firstAccess: só um booleano (instalação nova, admin com senha inicial).
	out := gin.H{"appName": defaultAppName, "logo": "", "firstAccess": h.service.FirstAccess()}
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
	// Username: usuário ou e-mail.
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password" binding:"required"`
}

func (h *AccessHandler) setSessionCookie(ctx *gin.Context, token string) {
	ctx.SetSameSite(http.SameSiteLaxMode)
	ctx.SetCookie(sessionCookie, token, int(12*60*60), "/", "", isHTTPS(ctx), true)
}

func (h *AccessHandler) loginResponse(ctx *gin.Context, identifier string, res *access_service.LoginResult, err error) {
	ci := clientInfo(ctx)
	if err != nil {
		var locked access_service.ErrLocked
		if errors.As(err, &locked) {
			h.service.Audit(access_service.AuditUsername(identifier), ci, access_service.AuditLoginFailed, identifier, "bloqueado por excesso de tentativas", false)
			ctx.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
			return
		}
		msg := "usuário ou senha inválidos"
		if errors.Is(err, access_service.ErrInvalidCode) {
			msg = "código inválido"
		} else if !errors.Is(err, access_service.ErrInvalidCredentials) {
			msg = err.Error()
		}
		h.service.Audit(access_service.AuditUsername(identifier), ci, access_service.AuditLoginFailed, identifier, msg, false)
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": msg})
		return
	}
	if res.TwoFactorRequired {
		ctx.JSON(http.StatusOK, gin.H{"twoFactorRequired": true, "challenge": res.Challenge})
		return
	}
	h.service.Audit(res.User, ci, access_service.AuditLogin, res.User.Username, "", true)
	h.setSessionCookie(ctx, res.Token)
	ctx.JSON(http.StatusOK, gin.H{"token": res.Token, "user": res.User})
}

func (h *AccessHandler) Login(ctx *gin.Context) {
	var body loginBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "usuário/e-mail e senha são obrigatórios"})
		return
	}
	identifier := strings.TrimSpace(body.Username)
	if identifier == "" {
		identifier = strings.TrimSpace(body.Email)
	}
	if identifier == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "usuário/e-mail e senha são obrigatórios"})
		return
	}
	res, err := h.service.Login(identifier, body.Password, clientInfo(ctx))
	h.loginResponse(ctx, identifier, res, err)
}

type twoFactorBody struct {
	Challenge string `json:"challenge" binding:"required"`
	Code      string `json:"code" binding:"required"`
}

func (h *AccessHandler) LoginTwoFactor(ctx *gin.Context) {
	var body twoFactorBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "challenge e code são obrigatórios"})
		return
	}
	res, err := h.service.LoginTwoFactor(body.Challenge, body.Code, clientInfo(ctx))
	h.loginResponse(ctx, "", res, err)
}

func (h *AccessHandler) Logout(ctx *gin.Context) {
	token := sessionToken(ctx)
	if token != "" {
		if user, _, err := h.service.ValidateSession(token); err == nil {
			h.service.Audit(user, clientInfo(ctx), access_service.AuditLogout, user.Username, "", true)
		}
		h.service.Logout(token)
	}
	ctx.SetCookie(sessionCookie, "", -1, "/", "", isHTTPS(ctx), true)
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
	if err := h.service.ChangePassword(currentUser(ctx).Id, body.CurrentPassword, body.NewPassword, currentSessionId(ctx)); err != nil {
		h.audit(ctx, access_service.AuditPasswordChange, currentUser(ctx).Username, err.Error(), false)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.audit(ctx, access_service.AuditPasswordChange, currentUser(ctx).Username, "outras sessões encerradas", true)
	ctx.JSON(http.StatusOK, gin.H{"message": "senha alterada; as outras sessões foram encerradas"})
}

// ── minhas sessões / 2FA ─────────────────────────────────────────

func (h *AccessHandler) MySessions(ctx *gin.Context) {
	list, err := h.service.ListSessions(currentUser(ctx).Id, currentSessionId(ctx))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *AccessHandler) RevokeMySession(ctx *gin.Context) {
	id := ctx.Param("sessionId")
	if err := h.service.RevokeOwnSession(currentUser(ctx).Id, id); err != nil {
		ctx.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	h.audit(ctx, access_service.AuditSessionRevoked, id, "pelo próprio usuário", true)
	ctx.JSON(http.StatusOK, gin.H{"message": "sessão encerrada"})
}

func (h *AccessHandler) TotpSetup(ctx *gin.Context) {
	secret, url, err := h.service.StartTotpSetup(currentUser(ctx).Id)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	qr := ""
	if png, err := qrcode.Encode(url, qrcode.Medium, 240); err == nil {
		qr = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	}
	ctx.JSON(http.StatusOK, gin.H{"secret": secret, "otpauthUrl": url, "qrCode": qr})
}

type codeBody struct {
	Code     string `json:"code" binding:"required"`
	Password string `json:"password"`
}

func (h *AccessHandler) TotpEnable(ctx *gin.Context) {
	var body codeBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "code é obrigatório"})
		return
	}
	if err := h.service.EnableTotp(currentUser(ctx).Id, body.Code); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.audit(ctx, access_service.Audit2FAEnabled, currentUser(ctx).Username, "", true)
	ctx.JSON(http.StatusOK, gin.H{"message": "verificação em duas etapas ligada"})
}

func (h *AccessHandler) TotpDisable(ctx *gin.Context) {
	var body codeBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "code é obrigatório"})
		return
	}
	if err := h.service.DisableTotp(currentUser(ctx).Id, body.Password, body.Code); err != nil {
		h.audit(ctx, access_service.Audit2FADisabled, currentUser(ctx).Username, err.Error(), false)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.audit(ctx, access_service.Audit2FADisabled, currentUser(ctx).Username, "", true)
	ctx.JSON(http.StatusOK, gin.H{"message": "verificação em duas etapas desligada"})
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
	h.audit(ctx, access_service.AuditInstanceCreate, inst.Name, inst.Id, true)
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
	h.audit(ctx, access_service.AuditInstanceDelete, instanceId, "", true)
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

func (h *AccessHandler) AdminCreateUser(ctx *gin.Context) {
	var body access_service.CreateUserInput
	if err := ctx.ShouldBindJSON(&body); err != nil || body.Password == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "usuário ou e-mail, e senha, são obrigatórios"})
		return
	}
	user, err := h.service.CreateUser(body)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.audit(ctx, access_service.AuditUserCreated, user.Username, "papel "+user.Role, true)
	ctx.JSON(http.StatusCreated, gin.H{"data": user})
}

func (h *AccessHandler) AdminUpdateUser(ctx *gin.Context) {
	var body access_service.UpdateUserInput
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}
	user, err := h.service.UpdateUser(ctx.Param("userId"), body, currentUser(ctx).Id)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	detail := fmt.Sprintf("papel=%s ativo=%t vêTodas=%t", user.Role, user.Active, user.SeeAllInstances)
	h.audit(ctx, access_service.AuditUserUpdated, user.Username, detail, true)
	ctx.JSON(http.StatusOK, gin.H{"data": user})
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
	h.audit(ctx, access_service.AuditUserGroups, h.usernameOf(ctx.Param("userId")), strings.Join(body.GroupIds, ","), true)
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
	h.audit(ctx, access_service.AuditPasswordReset, h.usernameOf(ctx.Param("userId")), "sessões encerradas", true)
	ctx.JSON(http.StatusOK, gin.H{"message": "senha redefinida (troca obrigatória no próximo login)"})
}

func (h *AccessHandler) AdminResetTotp(ctx *gin.Context) {
	if err := h.service.AdminResetTotp(ctx.Param("userId")); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.audit(ctx, access_service.Audit2FAReset, h.usernameOf(ctx.Param("userId")), "", true)
	ctx.JSON(http.StatusOK, gin.H{"message": "verificação em duas etapas removida; o usuário pode configurar de novo"})
}

func (h *AccessHandler) AdminUserSessions(ctx *gin.Context) {
	list, err := h.service.ListSessions(ctx.Param("userId"), currentSessionId(ctx))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *AccessHandler) AdminRevokeSession(ctx *gin.Context) {
	sess, err := h.service.RevokeSession(ctx.Param("sessionId"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.audit(ctx, access_service.AuditSessionRevoked, h.usernameOf(sess.UserId), "pelo admin, IP "+sess.IP, true)
	ctx.JSON(http.StatusOK, gin.H{"message": "sessão encerrada"})
}

func (h *AccessHandler) AdminDeleteUser(ctx *gin.Context) {
	name := h.usernameOf(ctx.Param("userId"))
	if err := h.service.DeleteUser(ctx.Param("userId"), currentUser(ctx).Id); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.audit(ctx, access_service.AuditUserDeleted, name, "", true)
	ctx.JSON(http.StatusOK, gin.H{"message": "usuário removido"})
}

func (h *AccessHandler) usernameOf(userId string) string {
	if u, err := h.service.GetUser(userId); err == nil {
		return u.Username
	}
	return userId
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
	h.audit(ctx, access_service.AuditGroupCreated, group.Name, body.LdapGroupDN, true)
	ctx.JSON(http.StatusCreated, gin.H{"data": group})
}

func (h *AccessHandler) AdminDeleteGroup(ctx *gin.Context) {
	if err := h.service.DeleteGroup(ctx.Param("groupId")); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.audit(ctx, access_service.AuditGroupDeleted, ctx.Param("groupId"), "", true)
	ctx.JSON(http.StatusOK, gin.H{"message": "grupo removido"})
}

func (h *AccessHandler) AdminRegenerateGroupApiKey(ctx *gin.Context) {
	group, err := h.service.RegenerateGroupApiKey(ctx.Param("groupId"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.audit(ctx, access_service.AuditGroupKey, group.Name, "", true)
	ctx.JSON(http.StatusOK, gin.H{"data": group})
}

// AdminLinkInstance: compartilha a instância com um grupo — todos os membros
// do grupo passam a ver e usar a instância.
func (h *AccessHandler) AdminLinkInstance(ctx *gin.Context) {
	if err := h.service.LinkInstance(ctx.Param("groupId"), ctx.Param("instanceId")); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.audit(ctx, access_service.AuditInstanceShared, ctx.Param("instanceId"), "grupo "+ctx.Param("groupId"), true)
	ctx.JSON(http.StatusOK, gin.H{"message": "instância vinculada ao grupo"})
}

func (h *AccessHandler) AdminUnlinkInstance(ctx *gin.Context) {
	if err := h.service.UnlinkInstance(ctx.Param("groupId"), ctx.Param("instanceId")); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.audit(ctx, access_service.AuditInstanceUnshar, ctx.Param("instanceId"), "grupo "+ctx.Param("groupId"), true)
	ctx.JSON(http.StatusOK, gin.H{"message": "vínculo removido"})
}

// AdminInstanceGroups: com quais grupos a instância está compartilhada.
func (h *AccessHandler) AdminInstanceGroups(ctx *gin.Context) {
	ids, err := h.service.GroupIdsForInstance(ctx.Param("instanceId"))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if ids == nil {
		ids = []string{}
	}
	ctx.JSON(http.StatusOK, gin.H{"data": ids})
}

// ── admin: auditoria ─────────────────────────────────────────────

func (h *AccessHandler) AdminAudit(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "100"))
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	list, total, err := h.service.ListAudit(access_repository.AuditFilter{
		Username: ctx.Query("username"),
		Action:   ctx.Query("action"),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"data": list, "total": total})
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
	detail := body.Value
	if strings.Contains(ctx.Param("key"), "password") {
		detail = "(segredo)"
	}
	h.audit(ctx, access_service.AuditSettingChanged, ctx.Param("key"), detail, true)
	ctx.JSON(http.StatusOK, gin.H{"message": "configuração salva"})
}

const maxLogoSize = 3 << 20 // 3 MiB

var allowedLogoTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
	"image/svg+xml": true,
}

var logoFileRe = regexp.MustCompile(`^branding-logo-[0-9a-f-]{36}\.(png|jpg|webp|svg)$`)

var logoExtTypes = map[string]string{
	"png": "image/png", "jpg": "image/jpeg", "webp": "image/webp", "svg": "image/svg+xml",
}

var logoHTTP = &http.Client{Timeout: 10 * time.Second}

// BrandingLogo (público): entrega a logo guardada no MinIO pelo próprio app,
// para funcionar mesmo com MINIO_ENDPOINT interno. Só aceita nomes gerados
// pelo upload; SVG vai com CSP "sandbox" para não executar script na origem do app.
func (h *AccessHandler) BrandingLogo(ctx *gin.Context) {
	file := ctx.Param("file")
	m := logoFileRe.FindStringSubmatch(file)
	if h.mediaStorage == nil || m == nil {
		ctx.Status(http.StatusNotFound)
		return
	}
	src, err := h.mediaStorage.GetURL(ctx.Request.Context(), "evolution-go-medias/"+file)
	if err != nil {
		ctx.Status(http.StatusNotFound)
		return
	}
	resp, err := logoHTTP.Get(src)
	if err != nil {
		ctx.Status(http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		ctx.Status(http.StatusNotFound)
		return
	}
	ctx.Header("Cache-Control", "public, max-age=86400")
	ctx.Header("X-Content-Type-Options", "nosniff")
	ctx.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	ctx.DataFromReader(http.StatusOK, -1, logoExtTypes[m[1]], io.LimitReader(resp.Body, maxLogoSize), nil)
}

// AdminUploadLogo: sobe a logo pro MinIO (MEDIA_STORAGE já usado pra mídia do
// WhatsApp) e salva a URL pública resultante em evogo_settings["branding.logo"].
func (h *AccessHandler) AdminUploadLogo(ctx *gin.Context) {
	if h.mediaStorage == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "MinIO não está configurado (MINIO_ENABLED=false)"})
		return
	}

	fileHeader, err := ctx.FormFile("logo")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "arquivo 'logo' é obrigatório"})
		return
	}
	if fileHeader.Size > maxLogoSize {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "arquivo maior que 3MB"})
		return
	}
	contentType := fileHeader.Header.Get("Content-Type")
	if !allowedLogoTypes[contentType] {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "formato inválido: use PNG, JPEG, WebP ou SVG"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "falha ao abrir o arquivo enviado"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "falha ao ler o arquivo enviado"})
		return
	}

	ext := "png"
	switch contentType {
	case "image/jpeg":
		ext = "jpg"
	case "image/webp":
		ext = "webp"
	case "image/svg+xml":
		ext = "svg"
	}
	fileName := fmt.Sprintf("branding-logo-%s.%s", uuid.NewString(), ext)

	// Store() devolve URL assinada (expira em 7 dias) e com o endereço do
	// MinIO (muitas vezes interno, ex.: http://minio:9000) — o navegador não
	// alcança. A logo é servida pelo próprio app em /access/branding/logo/:file.
	if _, err := h.mediaStorage.Store(ctx.Request.Context(), data, fileName, contentType); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "falha ao enviar pro MinIO: " + err.Error()})
		return
	}
	url := "/access/branding/logo/" + fileName

	if err := h.service.SetSetting("branding.logo", url); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"url": url})
}

func (h *AccessHandler) AdminTestLdap(ctx *gin.Context) {
	if err := h.service.TestLdap(); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "conexão LDAP bem-sucedida"})
}

type ldapUserTestBody struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// AdminTestLdapUser: autentica um usuário real no AD (sem criar nada) e
// mostra os grupos e o papel que ele receberia.
func (h *AccessHandler) AdminTestLdapUser(ctx *gin.Context) {
	var body ldapUserTestBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "username e password são obrigatórios"})
		return
	}
	res, err := h.service.TestLdapUser(body.Username, body.Password)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"data": res})
}
