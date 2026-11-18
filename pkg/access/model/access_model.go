package access_model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"

	AuthSourceLocal = "local"
	AuthSourceLdap  = "ldap"
)

// User de acesso ao Manager (não confundir com contatos WhatsApp).
type AccessUser struct {
	Id                 string    `json:"id" gorm:"type:uuid;primaryKey"`
	Username           string    `json:"username" gorm:"uniqueIndex;not null"`
	PasswordHash       string    `json:"-"`
	DisplayName        string    `json:"displayName"`
	Role               string    `json:"role" gorm:"default:user"`
	AuthSource         string    `json:"authSource" gorm:"default:local"`
	MustChangePassword bool      `json:"mustChangePassword" gorm:"default:false"`
	CreatedAt          time.Time `json:"createdAt" gorm:"autoCreateTime"`

	// E-mail: também serve para entrar (no lugar do username). Vazio = só username.
	Email string `json:"email" gorm:"index"`
	// Active=false bloqueia o login e derruba as sessões abertas.
	Active bool `json:"active" gorm:"default:true"`
	// SeeAllInstances: só vale para admin. true = vê todas as instâncias;
	// false = vê só as dos grupos dele (como um usuário comum).
	SeeAllInstances bool       `json:"seeAllInstances" gorm:"default:true"`
	LastLoginAt     *time.Time `json:"lastLoginAt,omitempty"`

	// 2FA (TOTP, app autenticador). Segredos cifrados (ver crypto.go).
	TotpEnabled       bool   `json:"totpEnabled" gorm:"default:false"`
	TotpSecret        string `json:"-"`
	TotpPendingSecret string `json:"-"`

	Groups []AccessGroup `json:"groups,omitempty" gorm:"many2many:evogo_user_groups;joinForeignKey:UserId;joinReferences:GroupId"`
}

// Sessão do Manager guardada no servidor: o navegador recebe um token
// aleatório e o banco guarda só o hash dele. Permite logout de verdade,
// listar as sessões abertas e derrubar uma sessão.
type AccessSession struct {
	Id         string     `json:"id" gorm:"type:uuid;primaryKey"`
	UserId     string     `json:"userId" gorm:"type:uuid;index"`
	TokenHash  string     `json:"-" gorm:"uniqueIndex;not null"`
	IP         string     `json:"ip"`
	UserAgent  string     `json:"userAgent"`
	CreatedAt  time.Time  `json:"createdAt" gorm:"autoCreateTime"`
	LastSeenAt time.Time  `json:"lastSeenAt"`
	ExpiresAt  time.Time  `json:"expiresAt" gorm:"index"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
	Current    bool       `json:"current" gorm:"-"`
}

func (AccessSession) TableName() string { return "evogo_sessions" }

func (s *AccessSession) BeforeCreate(tx *gorm.DB) error {
	if s.Id == "" {
		s.Id = uuid.NewString()
	}
	return nil
}

// Registro de auditoria: logins, falhas e mudanças administrativas.
type AccessAudit struct {
	Id        string    `json:"id" gorm:"type:uuid;primaryKey"`
	CreatedAt time.Time `json:"createdAt" gorm:"autoCreateTime;index"`
	UserId    string    `json:"userId,omitempty" gorm:"index"`
	Username  string    `json:"username,omitempty"`
	IP        string    `json:"ip,omitempty"`
	Action    string    `json:"action" gorm:"index"`
	Target    string    `json:"target,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	Success   bool      `json:"success"`
}

func (AccessAudit) TableName() string { return "evogo_audit_log" }

func (a *AccessAudit) BeforeCreate(tx *gorm.DB) error {
	if a.Id == "" {
		a.Id = uuid.NewString()
	}
	return nil
}

func (AccessUser) TableName() string { return "evogo_admin_users" }

func (u *AccessUser) BeforeCreate(tx *gorm.DB) error {
	if u.Id == "" {
		u.Id = uuid.NewString()
	}
	return nil
}

// Grupo de acesso (setor/empresa).
type AccessGroup struct {
	Id          string    `json:"id" gorm:"type:uuid;primaryKey"`
	Name        string    `json:"name" gorm:"uniqueIndex;not null"`
	LdapGroupDN string    `json:"ldapGroupDn"`
	// ApiKey autentica como o grupo (header apikey) em endpoints tipo
	// /instance/all — restrito às instâncias vinculadas a esse grupo, em vez
	// de exigir a GLOBAL_API_KEY (que vê tudo). Sem uniqueIndex no banco de
	// propósito: grupos já existentes antes dessa coluna existir ficam com ''
	// até o backfill rodar (ver migrate() em main.go) — índice único sobre
	// string vazia repetida quebraria o AutoMigrate.
	ApiKey    string    `json:"apiKey"`
	CreatedAt time.Time `json:"createdAt" gorm:"autoCreateTime"`
}

func (AccessGroup) TableName() string { return "evogo_groups" }

func (g *AccessGroup) BeforeCreate(tx *gorm.DB) error {
	if g.Id == "" {
		g.Id = uuid.NewString()
	}
	if g.ApiKey == "" {
		g.ApiKey = uuid.NewString()
	}
	return nil
}

// Vínculo usuário ↔ grupo.
type AccessUserGroup struct {
	UserId  string `json:"userId" gorm:"type:uuid;primaryKey"`
	GroupId string `json:"groupId" gorm:"type:uuid;primaryKey"`
}

func (AccessUserGroup) TableName() string { return "evogo_user_groups" }

// Vínculo grupo ↔ instância (dono da instância).
type AccessGroupInstance struct {
	GroupId    string `json:"groupId" gorm:"type:uuid;primaryKey"`
	InstanceId string `json:"instanceId" gorm:"type:uuid;primaryKey"`
}

func (AccessGroupInstance) TableName() string { return "evogo_group_instances" }

// Config chave/valor editável pela UI (ex.: LDAP na fase 2).
type AccessSetting struct {
	Key   string `json:"key" gorm:"primaryKey"`
	Value string `json:"value" gorm:"not null"`
}

func (AccessSetting) TableName() string { return "evogo_settings" }
