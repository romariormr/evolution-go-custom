package access_repository

import (
	access_model "github.com/EvolutionAPI/evolution-go/pkg/access/model"
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
	ListGroups() ([]*access_model.AccessGroup, error)
	DeleteGroup(id string) error

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

func (r *accessRepository) ListGroups() ([]*access_model.AccessGroup, error) {
	var groups []*access_model.AccessGroup
	err := r.db.Order("name").Find(&groups).Error
	return groups, err
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
