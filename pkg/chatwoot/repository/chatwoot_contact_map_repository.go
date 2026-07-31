package chatwoot_repository

import (
	"errors"

	chatwoot_model "github.com/EvolutionAPI/evolution-go/pkg/chatwoot/model"
	"gorm.io/gorm"
)

type ChatwootContactMapRepository interface {
	GetByJid(instanceId, jid string) (*chatwoot_model.ChatwootContactMap, error)
	Upsert(m *chatwoot_model.ChatwootContactMap) error
}

type chatwootContactMapRepository struct {
	db *gorm.DB
}

func NewChatwootContactMapRepository(db *gorm.DB) ChatwootContactMapRepository {
	return &chatwootContactMapRepository{db: db}
}

func (r *chatwootContactMapRepository) GetByJid(instanceId, jid string) (*chatwoot_model.ChatwootContactMap, error) {
	var m chatwoot_model.ChatwootContactMap
	err := r.db.Where("instance_id = ? AND jid = ?", instanceId, jid).First(&m).Error
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *chatwootContactMapRepository) Upsert(m *chatwoot_model.ChatwootContactMap) error {
	existing, err := r.GetByJid(m.InstanceId, m.Jid)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return r.db.Create(m).Error
	}

	m.Id = existing.Id
	m.CreatedAt = existing.CreatedAt
	if m.ChatwootContactId == "" {
		m.ChatwootContactId = existing.ChatwootContactId
	}
	if m.ChatwootConversationId == "" {
		m.ChatwootConversationId = existing.ChatwootConversationId
	}
	return r.db.Save(m).Error
}
