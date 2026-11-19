package access_service

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode"

	access_model "github.com/EvolutionAPI/evolution-go/pkg/access/model"
	access_repository "github.com/EvolutionAPI/evolution-go/pkg/access/repository"
	"github.com/EvolutionAPI/evolution-go/pkg/config"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("usuário ou senha inválidos")
	ErrInvalidToken       = errors.New("sessão inválida ou expirada")
	ErrForbidden          = errors.New("sem permissão")
	ErrInvalidCode        = errors.New("código inválido")
	ErrLastAdmin          = errors.New("precisa existir pelo menos um administrador ativo")
)

// ErrLocked: muitas tentativas erradas; Until diz até quando.
type ErrLocked struct{ Until time.Time }

func (e ErrLocked) Error() string {
	return fmt.Sprintf("muitas tentativas erradas; tente de novo depois das %s", e.Until.Local().Format("15:04"))
}

const (
	sessionTTL       = 12 * time.Hour
	sessionTouchStep = time.Minute
	issuerName       = "Evolution GO"
)

// ClientInfo: de onde veio a requisição (vai para a sessão e para a auditoria).
type ClientInfo struct {
	IP        string
	UserAgent string
}

type LoginResult struct {
	Token             string                    `json:"token,omitempty"`
	User              *access_model.AccessUser  `json:"user,omitempty"`
	Session           *access_model.AccessSession `json:"-"`
	TwoFactorRequired bool                      `json:"twoFactorRequired,omitempty"`
	Challenge         string                    `json:"challenge,omitempty"`
}

type CreateUserInput struct {
	Username        string   `json:"username"`
	Email           string   `json:"email"`
	Password        string   `json:"password"`
	DisplayName     string   `json:"displayName"`
	Role            string   `json:"role"`
	SeeAllInstances *bool    `json:"seeAllInstances"`
	GroupIds        []string `json:"groupIds"`
}

type UpdateUserInput struct {
	DisplayName     *string `json:"displayName"`
	Email           *string `json:"email"`
	Role            *string `json:"role"`
	Active          *bool   `json:"active"`
	SeeAllInstances *bool   `json:"seeAllInstances"`
}

type AccessService interface {
	Bootstrap() error
	Maintenance()
	FirstAccess() bool

	// login / sessão
	Login(identifier, password string, ci ClientInfo) (*LoginResult, error)
	LoginTwoFactor(challenge, code string, ci ClientInfo) (*LoginResult, error)
	ValidateSession(token string) (*access_model.AccessUser, *access_model.AccessSession, error)
	Logout(token string)
	ListSessions(userId, currentSessionId string) ([]*access_model.AccessSession, error)
	RevokeOwnSession(userId, sessionId string) error
	RevokeSession(sessionId string) (*access_model.AccessSession, error)
	ChangePassword(userId, current, newPassword, keepSessionId string) error

	// 2FA
	StartTotpSetup(userId string) (secret, otpauthURL string, err error)
	EnableTotp(userId, code string) error
	DisableTotp(userId, password, code string) error
	AdminResetTotp(userId string) error

	// admin
	CreateUser(in CreateUserInput) (*access_model.AccessUser, error)
	UpdateUser(id string, in UpdateUserInput, actorId string) (*access_model.AccessUser, error)
	GetUser(id string) (*access_model.AccessUser, error)
	ListUsers() ([]*access_model.AccessUser, error)
	UpdateUserGroups(userId string, groupIds []string) error
	ResetPassword(userId, newPassword string) error
	DeleteUser(id, actorId string) error
	CreateGroup(name, ldapGroupDN string) (*access_model.AccessGroup, error)
	ListGroups() ([]*access_model.AccessGroup, error)
	DeleteGroup(id string) error
	RegenerateGroupApiKey(id string) (*access_model.AccessGroup, error)
	LinkInstance(groupId, instanceId string) error
	UnlinkInstance(groupId, instanceId string) error
	GroupIdsForInstance(instanceId string) ([]string, error)
	ListSettings() (map[string]string, error)
	SetSetting(key, value string) error
	TestLdap() error
	TestLdapUser(username, password string) (*LdapTestResult, error)

	// auditoria
	Audit(actor *access_model.AccessUser, ci ClientInfo, action, target, detail string, success bool)
	ListAudit(f access_repository.AuditFilter) ([]*access_model.AccessAudit, int64, error)

	// escopo
	AllowedInstanceIds(user *access_model.AccessUser) (map[string]bool, bool, error)
	LinkInstanceToUserGroups(user *access_model.AccessUser, instanceId string, groupId string) error
	UnlinkInstanceEverywhere(instanceId string) error
}

type accessService struct {
	repo       access_repository.AccessRepository
	config     *config.Config
	guard      *loginGuard
	challenges *challengeStore
}

// globalKeyForSecrets: usado por loadLdapConfig (ldap_service.go) para decifrar.
var globalKeyForSecrets string

func NewAccessService(repo access_repository.AccessRepository, cfg *config.Config) AccessService {
	globalKeyForSecrets = cfg.GlobalApiKey
	return &accessService{repo: repo, config: cfg, guard: newLoginGuard(), challenges: newChallengeStore()}
}

func (s *accessService) Bootstrap() error {
	// Senha do LDAP gravada em texto puro por versões antigas → cifra.
	if v, err := s.repo.GetSetting(settingLdapBindPassword); err == nil && v != "" && !strings.HasPrefix(v, encPrefix) {
		if enc, err := encryptSecret(s.config.GlobalApiKey, v); err == nil {
			_ = s.repo.SetSetting(settingLdapBindPassword, enc)
			fmt.Println("[ACCESS] senha da conta de serviço do LDAP agora está cifrada no banco")
		}
	}

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
		Active:             true,
		SeeAllInstances:    true,
	}
	if err := s.repo.CreateUser(admin); err != nil {
		return err
	}
	fmt.Println("[ACCESS] usuário 'admin' criado (senha inicial = GLOBAL_API_KEY, troca obrigatória)")
	return nil
}

// FirstAccess: true enquanto o único usuário é o 'admin' criado no Bootstrap e
// ele ainda não trocou a senha inicial (= GLOBAL_API_KEY). A tela de login usa
// isso para explicar o primeiro acesso. Não expõe nenhum segredo.
func (s *accessService) FirstAccess() bool {
	if n, err := s.repo.CountUsers(); err != nil || n != 1 {
		return false
	}
	u, err := s.repo.GetUserByUsername("admin")
	return err == nil && u != nil && u.Active && u.MustChangePassword &&
		u.AuthSource == access_model.AuthSourceLocal
}

// Maintenance: apaga sessões vencidas (>7 dias) e auditoria antiga (AUDIT_RETENTION_DAYS, padrão 180).
func (s *accessService) Maintenance() {
	now := time.Now()
	_ = s.repo.PurgeSessions(now.Add(-7 * 24 * time.Hour))
	_ = s.repo.PurgeAudit(now.AddDate(0, 0, -auditRetentionDays()))
}

// ── senha ────────────────────────────────────────────────────────

// validatePassword: mínimo 8 caracteres, com letra e número, diferente do usuário/e-mail.
func validatePassword(pw, username, email string) error {
	if len(pw) < 8 {
		return errors.New("a senha precisa de pelo menos 8 caracteres")
	}
	var letter, digit bool
	for _, r := range pw {
		switch {
		case unicode.IsLetter(r):
			letter = true
		case unicode.IsDigit(r):
			digit = true
		}
	}
	if !letter || !digit {
		return errors.New("a senha precisa ter letras e números")
	}
	low := strings.ToLower(pw)
	if (username != "" && low == strings.ToLower(username)) || (email != "" && low == strings.ToLower(email)) {
		return errors.New("a senha não pode ser igual ao usuário ou e-mail")
	}
	return nil
}

func normalizeEmail(email string) (string, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return "", nil
	}
	if _, err := mail.ParseAddress(email); err != nil || !strings.Contains(email, "@") {
		return "", errors.New("e-mail inválido")
	}
	return email, nil
}

// ── login ────────────────────────────────────────────────────────

func (s *accessService) findUser(identifier string) (*access_model.AccessUser, error) {
	if strings.Contains(identifier, "@") {
		if u, err := s.repo.GetUserByEmail(identifier); err == nil {
			return u, nil
		}
	}
	return s.repo.GetUserByUsername(identifier)
}

func (s *accessService) Login(identifier, password string, ci ClientInfo) (*LoginResult, error) {
	identifier = strings.TrimSpace(strings.ToLower(identifier))
	now := time.Now()
	if until := s.guard.locked(identifier, ci.IP, now); !until.IsZero() {
		return nil, ErrLocked{Until: until}
	}

	u, err := s.findUser(identifier)
	switch {
	case err != nil:
		// Não existe localmente → tenta LDAP (provisiona no 1º acesso).
		u, err = s.loginViaLdap(identifier, password, nil)
	case u.AuthSource == access_model.AuthSourceLdap:
		u, err = s.loginViaLdap(u.Username, password, u)
	default:
		if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
			err = ErrInvalidCredentials
		}
	}
	if err == nil && !u.Active {
		err = ErrInvalidCredentials
	}
	if err != nil {
		s.guard.fail(identifier, ci.IP, now)
		return nil, ErrInvalidCredentials
	}
	s.guard.success(identifier, ci.IP)

	if u.TotpEnabled {
		return &LoginResult{TwoFactorRequired: true, Challenge: s.challenges.put(u.Id, identifier)}, nil
	}
	return s.openSession(u, ci)
}

func (s *accessService) LoginTwoFactor(chal, code string, ci ClientInfo) (*LoginResult, error) {
	c, ok := s.challenges.take(chal)
	if !ok {
		return nil, errors.New("o tempo para digitar o código acabou; entre de novo")
	}
	now := time.Now()
	if until := s.guard.locked(c.identifier, ci.IP, now); !until.IsZero() {
		s.challenges.drop(chal)
		return nil, ErrLocked{Until: until}
	}
	u, err := s.repo.GetUserById(c.userId)
	if err != nil || !u.Active || !u.TotpEnabled {
		s.challenges.drop(chal)
		return nil, ErrInvalidCredentials
	}
	secret, err := decryptSecret(s.config.GlobalApiKey, u.TotpSecret)
	if err != nil || !verifyTotp(u.Id, secret, code, now) {
		s.guard.fail(c.identifier, ci.IP, now)
		return nil, ErrInvalidCode
	}
	s.challenges.drop(chal)
	s.guard.success(c.identifier, ci.IP)
	return s.openSession(u, ci)
}

func (s *accessService) openSession(u *access_model.AccessUser, ci ClientInfo) (*LoginResult, error) {
	token := randomToken(32)
	now := time.Now()
	sess := &access_model.AccessSession{
		UserId:     u.Id,
		TokenHash:  hashToken(token),
		IP:         ci.IP,
		UserAgent:  clipStr(ci.UserAgent, 300),
		LastSeenAt: now,
		ExpiresAt:  now.Add(sessionTTL),
	}
	if err := s.repo.CreateSession(sess); err != nil {
		return nil, err
	}
	u.LastLoginAt = &now
	_ = s.repo.UpdateUser(u)
	return &LoginResult{Token: token, User: u, Session: sess}, nil
}

// loginViaLdap autentica no AD e cria (1ª vez) ou atualiza o usuário local,
// sincronizando grupos (memberOf ↔ AccessGroup.LdapGroupDN) e, se
// ldap.admin_group_dn estiver configurado, o papel de admin.
func (s *accessService) loginViaLdap(username, password string, existing *access_model.AccessUser) (*access_model.AccessUser, error) {
	_, displayName, groupDNs, email, err := AuthenticateLdap(s.repo, username, password)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	adminDN, _ := s.repo.GetSetting(settingLdapAdminGroupDN)

	u := existing
	if u == nil {
		u = &access_model.AccessUser{
			Username:        username,
			DisplayName:     displayName,
			Email:           strings.ToLower(email),
			Role:            access_model.RoleUser,
			AuthSource:      access_model.AuthSourceLdap,
			Active:          true,
			SeeAllInstances: true,
		}
		if adminDN != "" && matchGroupDNs(groupDNs, adminDN) {
			u.Role = access_model.RoleAdmin
		}
		if err := s.repo.CreateUser(u); err != nil {
			return nil, err
		}
	} else {
		changed := false
		if displayName != "" && displayName != u.DisplayName {
			u.DisplayName, changed = displayName, true
		}
		if email != "" && u.Email == "" {
			u.Email, changed = strings.ToLower(email), true
		}
		if adminDN != "" {
			role := access_model.RoleUser
			if matchGroupDNs(groupDNs, adminDN) {
				role = access_model.RoleAdmin
			}
			if role != u.Role {
				u.Role, changed = role, true
			}
		}
		if changed {
			_ = s.repo.UpdateUser(u)
		}
	}

	if err := s.syncLdapGroups(u.Id, groupDNs); err != nil {
		fmt.Printf("[ACCESS][LDAP] falha ao sincronizar grupos de %s: %v\n", username, err)
	}
	return s.repo.GetUserById(u.Id)
}

// syncLdapGroups vincula o usuário a todo AccessGroup cujo LdapGroupDN bate com algum memberOf do AD.
func (s *accessService) syncLdapGroups(userId string, groupDNs []string) error {
	allGroups, err := s.repo.ListGroups()
	if err != nil {
		return err
	}
	matched := make([]string, 0, len(allGroups))
	for _, g := range allGroups {
		if matchGroupDNs(groupDNs, g.LdapGroupDN) {
			matched = append(matched, g.Id)
		}
	}
	return s.repo.SetUserGroups(userId, matched)
}

// ── sessão ───────────────────────────────────────────────────────

func (s *accessService) ValidateSession(token string) (*access_model.AccessUser, *access_model.AccessSession, error) {
	if token == "" {
		return nil, nil, ErrInvalidToken
	}
	sess, err := s.repo.GetSessionByHash(hashToken(token))
	if err != nil || sess.RevokedAt != nil || time.Now().After(sess.ExpiresAt) {
		return nil, nil, ErrInvalidToken
	}
	u, err := s.repo.GetUserById(sess.UserId)
	if err != nil || !u.Active {
		return nil, nil, ErrInvalidToken
	}
	if now := time.Now(); now.Sub(sess.LastSeenAt) > sessionTouchStep {
		_ = s.repo.TouchSession(sess.Id, now)
		sess.LastSeenAt = now
	}
	return u, sess, nil
}

func (s *accessService) Logout(token string) {
	if token == "" {
		return
	}
	if sess, err := s.repo.GetSessionByHash(hashToken(token)); err == nil {
		_ = s.repo.RevokeSession(sess.Id)
	}
}

func (s *accessService) ListSessions(userId, currentSessionId string) ([]*access_model.AccessSession, error) {
	list, err := s.repo.ListSessions(userId)
	if err != nil {
		return nil, err
	}
	for _, x := range list {
		x.Current = x.Id == currentSessionId
	}
	return list, nil
}

func (s *accessService) RevokeOwnSession(userId, sessionId string) error {
	sess, err := s.repo.GetSession(sessionId)
	if err != nil || sess.UserId != userId {
		return ErrForbidden
	}
	return s.repo.RevokeSession(sessionId)
}

func (s *accessService) RevokeSession(sessionId string) (*access_model.AccessSession, error) {
	sess, err := s.repo.GetSession(sessionId)
	if err != nil {
		return nil, err
	}
	return sess, s.repo.RevokeSession(sessionId)
}

func (s *accessService) ChangePassword(userId, current, newPassword, keepSessionId string) error {
	u, err := s.repo.GetUserById(userId)
	if err != nil {
		return ErrInvalidCredentials
	}
	if u.AuthSource == access_model.AuthSourceLdap {
		return errors.New("usuário do AD: troque a senha no Active Directory")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)) != nil {
		return ErrInvalidCredentials
	}
	if err := validatePassword(newPassword, u.Username, u.Email); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	u.MustChangePassword = false
	if err := s.repo.UpdateUser(u); err != nil {
		return err
	}
	// Senha nova: derruba as outras sessões abertas (fica só esta).
	return s.repo.RevokeUserSessions(u.Id, keepSessionId)
}

// ── 2FA ──────────────────────────────────────────────────────────

func (s *accessService) StartTotpSetup(userId string) (string, string, error) {
	u, err := s.repo.GetUserById(userId)
	if err != nil {
		return "", "", err
	}
	if u.TotpEnabled {
		return "", "", errors.New("a verificação em duas etapas já está ligada")
	}
	secret := newTotpSecret()
	enc, err := encryptSecret(s.config.GlobalApiKey, secret)
	if err != nil {
		return "", "", err
	}
	u.TotpPendingSecret = enc
	if err := s.repo.UpdateUser(u); err != nil {
		return "", "", err
	}
	account := u.Username
	if u.Email != "" {
		account = u.Email
	}
	return secret, totpURL(issuerName, account, secret), nil
}

func (s *accessService) EnableTotp(userId, code string) error {
	u, err := s.repo.GetUserById(userId)
	if err != nil {
		return err
	}
	if u.TotpPendingSecret == "" {
		return errors.New("comece a configuração de novo")
	}
	secret, err := decryptSecret(s.config.GlobalApiKey, u.TotpPendingSecret)
	if err != nil {
		return err
	}
	if !verifyTotp(u.Id, secret, code, time.Now()) {
		return ErrInvalidCode
	}
	u.TotpSecret = u.TotpPendingSecret
	u.TotpPendingSecret = ""
	u.TotpEnabled = true
	return s.repo.UpdateUser(u)
}

func (s *accessService) DisableTotp(userId, password, code string) error {
	u, err := s.repo.GetUserById(userId)
	if err != nil {
		return err
	}
	if !u.TotpEnabled {
		return nil
	}
	if u.AuthSource == access_model.AuthSourceLocal && bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return ErrInvalidCredentials
	}
	secret, err := decryptSecret(s.config.GlobalApiKey, u.TotpSecret)
	if err != nil || !verifyTotp(u.Id, secret, code, time.Now()) {
		return ErrInvalidCode
	}
	u.TotpEnabled, u.TotpSecret, u.TotpPendingSecret = false, "", ""
	return s.repo.UpdateUser(u)
}

func (s *accessService) AdminResetTotp(userId string) error {
	u, err := s.repo.GetUserById(userId)
	if err != nil {
		return err
	}
	u.TotpEnabled, u.TotpSecret, u.TotpPendingSecret = false, "", ""
	if err := s.repo.UpdateUser(u); err != nil {
		return err
	}
	return s.repo.RevokeUserSessions(u.Id, "")
}

// ── admin CRUD ───────────────────────────────────────────────────

func (s *accessService) CreateUser(in CreateUserInput) (*access_model.AccessUser, error) {
	username := strings.TrimSpace(strings.ToLower(in.Username))
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return nil, err
	}
	if username == "" {
		username = email // perfil criado só com e-mail: o e-mail vira o usuário
	}
	if username == "" {
		return nil, errors.New("informe usuário ou e-mail")
	}
	if email != "" {
		if _, err := s.repo.GetUserByEmail(email); err == nil {
			return nil, errors.New("já existe um usuário com esse e-mail")
		}
	}
	if _, err := s.repo.GetUserByUsername(username); err == nil {
		return nil, errors.New("já existe um usuário com esse nome")
	}
	if err := validatePassword(in.Password, username, email); err != nil {
		return nil, err
	}
	role := access_model.RoleUser
	if in.Role == access_model.RoleAdmin {
		role = access_model.RoleAdmin
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	u := &access_model.AccessUser{
		Username:           username,
		Email:              email,
		PasswordHash:       string(hash),
		DisplayName:        strings.TrimSpace(in.DisplayName),
		Role:               role,
		AuthSource:         access_model.AuthSourceLocal,
		MustChangePassword: true,
		Active:             true,
		SeeAllInstances:    true,
	}
	if err := s.repo.CreateUser(u); err != nil {
		return nil, err
	}
	// GORM aplica default:true em bool false na criação; o false vai num update.
	if in.SeeAllInstances != nil && !*in.SeeAllInstances {
		u.SeeAllInstances = false
		if err := s.repo.UpdateUser(u); err != nil {
			return nil, err
		}
	}
	if len(in.GroupIds) > 0 {
		if err := s.repo.SetUserGroups(u.Id, in.GroupIds); err != nil {
			return nil, err
		}
	}
	return s.repo.GetUserById(u.Id)
}

func (s *accessService) UpdateUser(id string, in UpdateUserInput, actorId string) (*access_model.AccessUser, error) {
	u, err := s.repo.GetUserById(id)
	if err != nil {
		return nil, err
	}
	wasActiveAdmin := u.Role == access_model.RoleAdmin && u.Active
	if in.DisplayName != nil {
		u.DisplayName = strings.TrimSpace(*in.DisplayName)
	}
	if in.Email != nil {
		email, err := normalizeEmail(*in.Email)
		if err != nil {
			return nil, err
		}
		if email != "" && email != u.Email {
			if other, err := s.repo.GetUserByEmail(email); err == nil && other.Id != u.Id {
				return nil, errors.New("já existe um usuário com esse e-mail")
			}
		}
		u.Email = email
	}
	if in.Role != nil {
		if *in.Role == access_model.RoleAdmin {
			u.Role = access_model.RoleAdmin
		} else {
			u.Role = access_model.RoleUser
		}
	}
	if in.Active != nil {
		u.Active = *in.Active
	}
	if in.SeeAllInstances != nil {
		u.SeeAllInstances = *in.SeeAllInstances
	}
	if id == actorId && (!u.Active || u.Role != access_model.RoleAdmin) {
		return nil, errors.New("você não pode desativar nem tirar o admin do próprio usuário")
	}
	if wasActiveAdmin && (!u.Active || u.Role != access_model.RoleAdmin) {
		if n, err := s.repo.CountActiveAdmins(); err == nil && n <= 1 {
			return nil, ErrLastAdmin
		}
	}
	if err := s.repo.UpdateUser(u); err != nil {
		return nil, err
	}
	if !u.Active {
		_ = s.repo.RevokeUserSessions(u.Id, "")
	}
	return s.repo.GetUserById(u.Id)
}

func (s *accessService) GetUser(id string) (*access_model.AccessUser, error) { return s.repo.GetUserById(id) }
func (s *accessService) ListUsers() ([]*access_model.AccessUser, error)     { return s.repo.ListUsers() }

func (s *accessService) UpdateUserGroups(userId string, groupIds []string) error {
	if _, err := s.repo.GetUserById(userId); err != nil {
		return err
	}
	return s.repo.SetUserGroups(userId, groupIds)
}

func (s *accessService) ResetPassword(userId, newPassword string) error {
	u, err := s.repo.GetUserById(userId)
	if err != nil {
		return err
	}
	if u.AuthSource == access_model.AuthSourceLdap {
		return errors.New("usuário do AD: a senha é a do Active Directory")
	}
	if err := validatePassword(newPassword, u.Username, u.Email); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	u.MustChangePassword = true
	if err := s.repo.UpdateUser(u); err != nil {
		return err
	}
	return s.repo.RevokeUserSessions(u.Id, "")
}

func (s *accessService) DeleteUser(id, actorId string) error {
	if id == actorId {
		return errors.New("não é possível excluir o próprio usuário")
	}
	u, err := s.repo.GetUserById(id)
	if err != nil {
		return err
	}
	if u.Role == access_model.RoleAdmin && u.Active {
		if n, err := s.repo.CountActiveAdmins(); err == nil && n <= 1 {
			return ErrLastAdmin
		}
	}
	_ = s.repo.RevokeUserSessions(id, "")
	return s.repo.DeleteUser(id)
}

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
func (s *accessService) RegenerateGroupApiKey(id string) (*access_model.AccessGroup, error) {
	return s.repo.RegenerateGroupApiKey(id)
}

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

// ── settings ─────────────────────────────────────────────────────

// secretSettings: nunca saem em claro pela API e ficam cifrados no banco.
var secretSettings = map[string]bool{settingLdapBindPassword: true}

const maskedSecret = "********"

func (s *accessService) ListSettings() (map[string]string, error) {
	all, err := s.repo.ListSettings()
	if err != nil {
		return nil, err
	}
	for k := range secretSettings {
		if all[k] != "" {
			all[k] = maskedSecret
		}
	}
	return all, nil
}

func (s *accessService) SetSetting(key, value string) error {
	key = strings.TrimSpace(key)
	if !(strings.HasPrefix(key, "ldap.") || strings.HasPrefix(key, "branding.")) {
		return errors.New("configuração desconhecida")
	}
	if secretSettings[key] {
		if value == maskedSecret {
			return nil // tela devolveu a máscara: mantém o valor atual
		}
		enc, err := encryptSecret(s.config.GlobalApiKey, value)
		if err != nil {
			return err
		}
		value = enc
	}
	return s.repo.SetSetting(key, value)
}

func (s *accessService) TestLdap() error { return TestLdapSettings(s.repo) }

func (s *accessService) TestLdapUser(username, password string) (*LdapTestResult, error) {
	return TestLdapUserLogin(s.repo, username, password)
}

// ── escopo por grupo ─────────────────────────────────────────────

// AllowedInstanceIds: (ids permitidos, vêTudo). Admin com SeeAllInstances →
// mapa nil e true (sem filtro). Admin sem SeeAllInstances e usuário comum →
// só as instâncias dos grupos dele.
func (s *accessService) AllowedInstanceIds(user *access_model.AccessUser) (map[string]bool, bool, error) {
	if user == nil || (user.Role == access_model.RoleAdmin && user.SeeAllInstances) {
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
			if user != nil && !user.SeeAllInstances {
				// admin que só vê os próprios grupos: sem grupo, a instância sumiria da tela dele
				if ids, _ := s.repo.GroupIdsForUser(user.Id); len(ids) > 0 {
					return s.LinkInstance(ids[0], instanceId)
				}
			}
			return nil // admin sem grupo → instância sem dono (visível só p/ admins que veem tudo)
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

func clipStr(v string, n int) string {
	if len(v) <= n {
		return v
	}
	return v[:n]
}
