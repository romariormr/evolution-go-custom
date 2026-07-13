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

	Groups []AccessGroup `json:"groups,omitempty" gorm:"many2many:evogo_user_groups;joinForeignKey:UserId;joinReferences:GroupId"`
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
	CreatedAt   time.Time `json:"createdAt" gorm:"autoCreateTime"`
}

func (AccessGroup) TableName() string { return "evogo_groups" }

func (g *AccessGroup) BeforeCreate(tx *gorm.DB) error {
	if g.Id == "" {
		g.Id = uuid.NewString()
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
