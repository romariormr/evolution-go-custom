package access_repository

import (
	"strings"
	"time"

	access_model "github.com/EvolutionAPI/evolution-go/pkg/access/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AccessRepository interface {
	// users
	CountUsers() (int64, error)
	CreateUser(user *access_model.AccessUser) error
	GetUserById(id string) (*access_model.AccessUser, error)
	GetUserByUsername(username string) (*access_model.AccessUser, error)
	ListUsers() ([]*access_model.AccessUser, error)
	UpdateUser(user *access_model.AccessUser) error
	DeleteUser(id string) error

	// groups
	CreateGroup(group *access_model.AccessGroup) error
	GetGroupById(id string) (*access_model.AccessGroup, error)
	GetGroupByApiKey(apiKey string) (*access_model.AccessGroup, error)
	ListGroups() ([]*access_model.AccessGroup, error)
	DeleteGroup(id string) error
	RegenerateGroupApiKey(id string) (*access_model.AccessGroup, error)
	// BackfillGroupApiKeys gera ApiKey pra grupos criados antes desse campo
	// existir (ficam com '' até então) — chamada 1x no boot, depois de
	// AutoMigrate (ver migrate() em main.go).
	BackfillGroupApiKeys() error

	// user<->group
	SetUserGroups(userId string, groupIds []string) error
	GroupIdsForUser(userId string) ([]string, error)

	// group<->instance
	LinkInstance(groupId, instanceId string) error
	UnlinkInstance(groupId, instanceId string) error
	InstanceIdsForGroups(groupIds []string) ([]string, error)
	GroupIdsForInstance(instanceId string) ([]string, error)
	UnlinkInstanceEverywhere(instanceId string) error

	// settings
	GetSetting(key string) (string, error)
	SetSetting(key, value string) error
	ListSettings() (map[string]string, error)

	// login por e-mail
	GetUserByEmail(email string) (*access_model.AccessUser, error)
	CountActiveAdmins() (int64, error)

	// sessões
	CreateSession(s *access_model.AccessSession) error
	GetSessionByHash(hash string) (*access_model.AccessSession, error)
	TouchSession(id string, at time.Time) error
	ListSessions(userId string) ([]*access_model.AccessSession, error)
	GetSession(id string) (*access_model.AccessSession, error)
	RevokeSession(id string) error
	RevokeUserSessions(userId, exceptId string) error
	PurgeSessions(before time.Time) error

	// auditoria
	AddAudit(a *access_model.AccessAudit) error
	ListAudit(f AuditFilter) ([]*access_model.AccessAudit, int64, error)
	PurgeAudit(before time.Time) error
}

type AuditFilter struct {
	Username string
	Action   string
	Limit    int
	Offset   int
}

type accessRepository struct {
	db *gorm.DB
}

func NewAccessRepository(db *gorm.DB) AccessRepository {
	return &accessRepository{db: db}
}

// ── users ────────────────────────────────────────────────────────

func (r *accessRepository) CountUsers() (int64, error) {
	var n int64
	err := r.db.Model(&access_model.AccessUser{}).Count(&n).Error
	return n, err
}

func (r *accessRepository) CreateUser(user *access_model.AccessUser) error {
	return r.db.Create(user).Error
}

func (r *accessRepository) GetUserById(id string) (*access_model.AccessUser, error) {
	var u access_model.AccessUser
	if err := r.db.Preload("Groups").Where("id = ?", id).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *accessRepository) GetUserByUsername(username string) (*access_model.AccessUser, error) {
	var u access_model.AccessUser
	if err := r.db.Preload("Groups").Where("username = ?", username).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *accessRepository) ListUsers() ([]*access_model.AccessUser, error) {
	var users []*access_model.AccessUser
	err := r.db.Preload("Groups").Order("username").Find(&users).Error
	return users, err
}

func (r *accessRepository) UpdateUser(user *access_model.AccessUser) error {
	return r.db.Save(user).Error
}

func (r *accessRepository) DeleteUser(id string) error {
	if err := r.db.Where("user_id = ?", id).Delete(&access_model.AccessUserGroup{}).Error; err != nil {
		return err
	}
	return r.db.Where("id = ?", id).Delete(&access_model.AccessUser{}).Error
}

// ── groups ───────────────────────────────────────────────────────

func (r *accessRepository) CreateGroup(group *access_model.AccessGroup) error {
	return r.db.Create(group).Error
}

func (r *accessRepository) GetGroupById(id string) (*access_model.AccessGroup, error) {
	var g access_model.AccessGroup
	if err := r.db.Where("id = ?", id).First(&g).Error; err != nil {
		return nil, err
	}
	return &g, nil
}

func (r *accessRepository) GetGroupByApiKey(apiKey string) (*access_model.AccessGroup, error) {
	if apiKey == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var g access_model.AccessGroup
	if err := r.db.Where("api_key = ?", apiKey).First(&g).Error; err != nil {
		return nil, err
	}
	return &g, nil
}

func (r *accessRepository) ListGroups() ([]*access_model.AccessGroup, error) {
	var groups []*access_model.AccessGroup
	err := r.db.Order("name").Find(&groups).Error
	return groups, err
}

func (r *accessRepository) RegenerateGroupApiKey(id string) (*access_model.AccessGroup, error) {
	g, err := r.GetGroupById(id)
	if err != nil {
		return nil, err
	}
	g.ApiKey = uuid.NewString()
	if err := r.db.Model(&access_model.AccessGroup{}).Where("id = ?", id).Update("api_key", g.ApiKey).Error; err != nil {
		return nil, err
	}
	return g, nil
}

func (r *accessRepository) BackfillGroupApiKeys() error {
	var groups []*access_model.AccessGroup
	if err := r.db.Where("api_key = '' OR api_key IS NULL").Find(&groups).Error; err != nil {
		return err
	}
	for _, g := range groups {
		if err := r.db.Model(&access_model.AccessGroup{}).Where("id = ?", g.Id).
			Update("api_key", uuid.NewString()).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *accessRepository) DeleteGroup(id string) error {
	if err := r.db.Where("group_id = ?", id).Delete(&access_model.AccessUserGroup{}).Error; err != nil {
		return err
	}
	if err := r.db.Where("group_id = ?", id).Delete(&access_model.AccessGroupInstance{}).Error; err != nil {
		return err
	}
	return r.db.Where("id = ?", id).Delete(&access_model.AccessGroup{}).Error
}

// ── user<->group ─────────────────────────────────────────────────

func (r *accessRepository) SetUserGroups(userId string, groupIds []string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userId).Delete(&access_model.AccessUserGroup{}).Error; err != nil {
			return err
		}
		for _, gid := range groupIds {
			if err := tx.Create(&access_model.AccessUserGroup{UserId: userId, GroupId: gid}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *accessRepository) GroupIdsForUser(userId string) ([]string, error) {
	var ids []string
	err := r.db.Model(&access_model.AccessUserGroup{}).
		Where("user_id = ?", userId).Pluck("group_id", &ids).Error
	return ids, err
}

// ── group<->instance ─────────────────────────────────────────────

func (r *accessRepository) LinkInstance(groupId, instanceId string) error {
	link := access_model.AccessGroupInstance{GroupId: groupId, InstanceId: instanceId}
	return r.db.Where(&link).FirstOrCreate(&link).Error
}

func (r *accessRepository) UnlinkInstance(groupId, instanceId string) error {
	return r.db.Where("group_id = ? AND instance_id = ?", groupId, instanceId).
		Delete(&access_model.AccessGroupInstance{}).Error
}

func (r *accessRepository) InstanceIdsForGroups(groupIds []string) ([]string, error) {
	if len(groupIds) == 0 {
		return []string{}, nil
	}
	var ids []string
	err := r.db.Model(&access_model.AccessGroupInstance{}).
		Where("group_id IN ?", groupIds).Distinct().Pluck("instance_id", &ids).Error
	return ids, err
}

func (r *accessRepository) GroupIdsForInstance(instanceId string) ([]string, error) {
	var ids []string
	err := r.db.Model(&access_model.AccessGroupInstance{}).
		Where("instance_id = ?", instanceId).Pluck("group_id", &ids).Error
	return ids, err
}

func (r *accessRepository) UnlinkInstanceEverywhere(instanceId string) error {
	return r.db.Where("instance_id = ?", instanceId).
		Delete(&access_model.AccessGroupInstance{}).Error
}

// ── settings ─────────────────────────────────────────────────────

func (r *accessRepository) GetSetting(key string) (string, error) {
	var s access_model.AccessSetting
	if err := r.db.Where("key = ?", key).First(&s).Error; err != nil {
		return "", err
	}
	return s.Value, nil
}

func (r *accessRepository) SetSetting(key, value string) error {
	s := access_model.AccessSetting{Key: key, Value: value}
	return r.db.Save(&s).Error
}

// ── login por e-mail / admins ────────────────────────────────────

func (r *accessRepository) GetUserByEmail(email string) (*access_model.AccessUser, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var u access_model.AccessUser
	if err := r.db.Preload("Groups").Where("LOWER(email) = ?", email).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *accessRepository) CountActiveAdmins() (int64, error) {
	var n int64
	err := r.db.Model(&access_model.AccessUser{}).
		Where("role = ? AND active = ?", access_model.RoleAdmin, true).Count(&n).Error
	return n, err
}

// ── sessões ──────────────────────────────────────────────────────

func (r *accessRepository) CreateSession(s *access_model.AccessSession) error {
	return r.db.Create(s).Error
}

func (r *accessRepository) GetSessionByHash(hash string) (*access_model.AccessSession, error) {
	var s access_model.AccessSession
	if err := r.db.Where("token_hash = ?", hash).First(&s).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *accessRepository) TouchSession(id string, at time.Time) error {
	return r.db.Model(&access_model.AccessSession{}).Where("id = ?", id).Update("last_seen_at", at).Error
}

// ListSessions: sessões ainda válidas (não revogadas, não expiradas), mais recentes primeiro.
func (r *accessRepository) ListSessions(userId string) ([]*access_model.AccessSession, error) {
	var out []*access_model.AccessSession
	err := r.db.Where("user_id = ? AND revoked_at IS NULL AND expires_at > ?", userId, time.Now()).
		Order("last_seen_at DESC").Find(&out).Error
	return out, err
}

func (r *accessRepository) GetSession(id string) (*access_model.AccessSession, error) {
	var s access_model.AccessSession
	if err := r.db.Where("id = ?", id).First(&s).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *accessRepository) RevokeSession(id string) error {
	return r.db.Model(&access_model.AccessSession{}).
		Where("id = ? AND revoked_at IS NULL", id).Update("revoked_at", time.Now()).Error
}

func (r *accessRepository) RevokeUserSessions(userId, exceptId string) error {
	q := r.db.Model(&access_model.AccessSession{}).Where("user_id = ? AND revoked_at IS NULL", userId)
	if exceptId != "" {
		q = q.Where("id <> ?", exceptId)
	}
	return q.Update("revoked_at", time.Now()).Error
}

// PurgeSessions apaga sessões expiradas/revogadas antes de "before".
func (r *accessRepository) PurgeSessions(before time.Time) error {
	return r.db.Where("expires_at < ? OR (revoked_at IS NOT NULL AND revoked_at < ?)", before, before).
		Delete(&access_model.AccessSession{}).Error
}

// ── auditoria ────────────────────────────────────────────────────

func (r *accessRepository) AddAudit(a *access_model.AccessAudit) error {
	return r.db.Create(a).Error
}

func (r *accessRepository) ListAudit(f AuditFilter) ([]*access_model.AccessAudit, int64, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	q := r.db.Model(&access_model.AccessAudit{})
	if f.Username != "" {
		q = q.Where("LOWER(username) LIKE ?", "%"+strings.ToLower(f.Username)+"%")
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var out []*access_model.AccessAudit
	err := q.Order("created_at DESC").Limit(f.Limit).Offset(f.Offset).Find(&out).Error
	return out, total, err
}

func (r *accessRepository) PurgeAudit(before time.Time) error {
	return r.db.Where("created_at < ?", before).Delete(&access_model.AccessAudit{}).Error
}

func (r *accessRepository) ListSettings() (map[string]string, error) {
	var all []access_model.AccessSetting
	if err := r.db.Find(&all).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(all))
	for _, s := range all {
		out[s.Key] = s.Value
	}
	return out, nil
}
