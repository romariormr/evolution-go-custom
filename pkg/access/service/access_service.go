package access_service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	access_model "github.com/EvolutionAPI/evolution-go/pkg/access/model"
	access_repository "github.com/EvolutionAPI/evolution-go/pkg/access/repository"
	"github.com/EvolutionAPI/evolution-go/pkg/config"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("usuário ou senha inválidos")
	ErrInvalidToken       = errors.New("sessão inválida ou expirada")
	ErrForbidden          = errors.New("sem permissão")
)

const sessionTTL = 12 * time.Hour

type SessionClaims struct {
	Sub  string `json:"sub"`
	Usr  string `json:"usr"`
	Role string `json:"role"`
	Iat  int64  `json:"iat"`
	Exp  int64  `json:"exp"`
}

type AccessService interface {
	Bootstrap() error
	Login(username, password string) (string, *access_model.AccessUser, error)
	ValidateSession(token string) (*access_model.AccessUser, error)
	ChangePassword(userId, current, newPassword string) error

	// admin
	CreateUser(username, password, displayName, role string, groupIds []string) (*access_model.AccessUser, error)
	ListUsers() ([]*access_model.AccessUser, error)
	UpdateUserGroups(userId string, groupIds []string) error
	ResetPassword(userId, newPassword string) error
	DeleteUser(id string) error
	CreateGroup(name, ldapGroupDN string) (*access_model.AccessGroup, error)
	ListGroups() ([]*access_model.AccessGroup, error)
	DeleteGroup(id string) error
	LinkInstance(groupId, instanceId string) error
	UnlinkInstance(groupId, instanceId string) error
	GroupIdsForInstance(instanceId string) ([]string, error)
	ListSettings() (map[string]string, error)
	SetSetting(key, value string) error

	// escopo
	AllowedInstanceIds(user *access_model.AccessUser) (map[string]bool, bool, error)
	LinkInstanceToUserGroups(user *access_model.AccessUser, instanceId string, groupId string) error
	UnlinkInstanceEverywhere(instanceId string) error
}

type accessService struct {
	repo   access_repository.AccessRepository
	config *config.Config
}

func NewAccessService(repo access_repository.AccessRepository, cfg *config.Config) AccessService {
	return &accessService{repo: repo, config: cfg}
}

// Bootstrap: sem usuários → cria admin com senha = GLOBAL_API_KEY (troca obrigatória).
func (s *accessService) Bootstrap() error {
	n, err := s.repo.CountUsers()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(s.config.GlobalApiKey), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	admin := &access_model.AccessUser{
		Username:           "admin",
		PasswordHash:       string(hash),
		DisplayName:        "Administrador",
		Role:               access_model.RoleAdmin,
		AuthSource:         access_model.AuthSourceLocal,
		MustChangePassword: true,
	}
	if err := s.repo.CreateUser(admin); err != nil {
		return err
	}
	fmt.Println("[ACCESS] usuário 'admin' criado (senha inicial = GLOBAL_API_KEY, troca obrigatória)")
	return nil
}

// ── sessão / JWT (HMAC-SHA256, secret = GLOBAL_API_KEY) ─────────

func (s *accessService) sign(data string) string {
	mac := hmac.New(sha256.New, []byte(s.config.GlobalApiKey))
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *accessService) issueToken(u *access_model.AccessUser) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	now := time.Now()
	claims := SessionClaims{
		Sub:  u.Id,
		Usr:  u.Username,
		Role: u.Role,
		Iat:  now.Unix(),
		Exp:  now.Add(sessionTTL).Unix(),
	}
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	unsigned := header + "." + payload
	return unsigned + "." + s.sign(unsigned), nil
}

func (s *accessService) parseToken(token string) (*SessionClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}
	unsigned := parts[0] + "." + parts[1]
	expected := s.sign(unsigned)
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return nil, ErrInvalidToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var claims SessionClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, ErrInvalidToken
	}
	if time.Now().Unix() > claims.Exp {
		return nil, ErrInvalidToken
	}
	return &claims, nil
}

func (s *accessService) Login(username, password string) (string, *access_model.AccessUser, error) {
	u, err := s.repo.GetUserByUsername(strings.TrimSpace(strings.ToLower(username)))
	if err != nil {
		return "", nil, ErrInvalidCredentials
	}
	// fase 2: if u.AuthSource == ldap → bind LDAP aqui
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return "", nil, ErrInvalidCredentials
	}
	token, err := s.issueToken(u)
	if err != nil {
		return "", nil, err
	}
	return token, u, nil
}

func (s *accessService) ValidateSession(token string) (*access_model.AccessUser, error) {
	claims, err := s.parseToken(token)
	if err != nil {
		return nil, err
	}
	u, err := s.repo.GetUserById(claims.Sub)
	if err != nil {
		return nil, ErrInvalidToken
	}
	return u, nil
}

func (s *accessService) ChangePassword(userId, current, newPassword string) error {
	u, err := s.repo.GetUserById(userId)
	if err != nil {
		return ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)) != nil {
		return ErrInvalidCredentials
	}
	if len(newPassword) < 8 {
		return errors.New("nova senha precisa de pelo menos 8 caracteres")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	u.MustChangePassword = false
	return s.repo.UpdateUser(u)
}

// ── admin CRUD ───────────────────────────────────────────────────

func (s *accessService) CreateUser(username, password, displayName, role string, groupIds []string) (*access_model.AccessUser, error) {
	username = strings.TrimSpace(strings.ToLower(username))
	if username == "" || len(password) < 8 {
		return nil, errors.New("username obrigatório e senha com pelo menos 8 caracteres")
	}
	if role != access_model.RoleAdmin {
		role = access_model.RoleUser
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	u := &access_model.AccessUser{
		Username:     username,
		PasswordHash: string(hash),
		DisplayName:  displayName,
		Role:         role,
		AuthSource:   access_model.AuthSourceLocal,
	}
	if err := s.repo.CreateUser(u); err != nil {
		return nil, err
	}
	if len(groupIds) > 0 {
		if err := s.repo.SetUserGroups(u.Id, groupIds); err != nil {
			return nil, err
		}
	}
	return s.repo.GetUserById(u.Id)
}

func (s *accessService) ListUsers() ([]*access_model.AccessUser, error) { return s.repo.ListUsers() }

func (s *accessService) UpdateUserGroups(userId string, groupIds []string) error {
	if _, err := s.repo.GetUserById(userId); err != nil {
		return err
	}
	return s.repo.SetUserGroups(userId, groupIds)
}

func (s *accessService) ResetPassword(userId, newPassword string) error {
	if len(newPassword) < 8 {
		return errors.New("senha precisa de pelo menos 8 caracteres")
	}
	u, err := s.repo.GetUserById(userId)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	u.MustChangePassword = true
	return s.repo.UpdateUser(u)
}

func (s *accessService) DeleteUser(id string) error { return s.repo.DeleteUser(id) }

func (s *accessService) CreateGroup(name, ldapGroupDN string) (*access_model.AccessGroup, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("nome do grupo obrigatório")
	}
	g := &access_model.AccessGroup{Name: name, LdapGroupDN: ldapGroupDN}
	if err := s.repo.CreateGroup(g); err != nil {
		return nil, err
	}
	return g, nil
}

func (s *accessService) ListGroups() ([]*access_model.AccessGroup, error) { return s.repo.ListGroups() }
func (s *accessService) DeleteGroup(id string) error                     { return s.repo.DeleteGroup(id) }

func (s *accessService) LinkInstance(groupId, instanceId string) error {
	if _, err := s.repo.GetGroupById(groupId); err != nil {
		return err
	}
	return s.repo.LinkInstance(groupId, instanceId)
}

func (s *accessService) UnlinkInstance(groupId, instanceId string) error {
	return s.repo.UnlinkInstance(groupId, instanceId)
}

func (s *accessService) GroupIdsForInstance(instanceId string) ([]string, error) {
	return s.repo.GroupIdsForInstance(instanceId)
}

func (s *accessService) ListSettings() (map[string]string, error) { return s.repo.ListSettings() }
func (s *accessService) SetSetting(key, value string) error       { return s.repo.SetSetting(key, value) }

// ── escopo por grupo ─────────────────────────────────────────────

// AllowedInstanceIds: (ids permitidos, éAdmin). Admin → mapa nil e true (sem filtro).
func (s *accessService) AllowedInstanceIds(user *access_model.AccessUser) (map[string]bool, bool, error) {
	if user == nil || user.Role == access_model.RoleAdmin {
		return nil, true, nil
	}
	groupIds, err := s.repo.GroupIdsForUser(user.Id)
	if err != nil {
		return nil, false, err
	}
	instanceIds, err := s.repo.InstanceIdsForGroups(groupIds)
	if err != nil {
		return nil, false, err
	}
	allowed := make(map[string]bool, len(instanceIds))
	for _, id := range instanceIds {
		allowed[id] = true
	}
	return allowed, false, nil
}

// LinkInstanceToUserGroups: vincula instância criada ao grupo informado
// (usuário comum só pode vincular a grupo do qual participa; vazio = primeiro grupo dele).
func (s *accessService) LinkInstanceToUserGroups(user *access_model.AccessUser, instanceId string, groupId string) error {
	if user == nil || user.Role == access_model.RoleAdmin {
		if groupId == "" {
			return nil // admin sem grupo → instância sem dono (visível só p/ admins)
		}
		return s.LinkInstance(groupId, instanceId)
	}
	groupIds, err := s.repo.GroupIdsForUser(user.Id)
	if err != nil {
		return err
	}
	if len(groupIds) == 0 {
		return errors.New("usuário não pertence a nenhum grupo — peça ao admin para te adicionar a um grupo")
	}
	if groupId == "" {
		groupId = groupIds[0]
	} else {
		ok := false
		for _, gid := range groupIds {
			if gid == groupId {
				ok = true
				break
			}
		}
		if !ok {
			return ErrForbidden
		}
	}
	return s.repo.LinkInstance(groupId, instanceId)
}

func (s *accessService) UnlinkInstanceEverywhere(instanceId string) error {
	return s.repo.UnlinkInstanceEverywhere(instanceId)
}
