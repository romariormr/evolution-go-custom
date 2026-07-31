package chatwoot_repository

import (
	"errors"

	chatwoot_model "github.com/EvolutionAPI/evolution-go/pkg/chatwoot/model"
	"gorm.io/gorm"
)

type ChatwootRepository interface {
	GetByInstanceId(instanceId string) (*chatwoot_model.ChatwootConfig, error)
	Upsert(cfg *chatwoot_model.ChatwootConfig) error
	Delete(instanceId string) error
	// ClearQrConversation esquece a conversa de status cacheada, sem mexer no
	// resto da config (InboxId, credenciais etc) — usado quando a conversa
	// cacheada ficou errada (ex.: casou com um contato real por engano) e
	// precisa ser recriada do zero na próxima notificação.
	ClearQrConversation(instanceId string) error
}

type chatwootRepository struct {
	db *gorm.DB
}

func NewChatwootRepository(db *gorm.DB) ChatwootRepository {
	return &chatwootRepository{db: db}
}

func (r *chatwootRepository) GetByInstanceId(instanceId string) (*chatwoot_model.ChatwootConfig, error) {
	var cfg chatwoot_model.ChatwootConfig
	err := r.db.Where("instance_id = ?", instanceId).First(&cfg).Error
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Upsert cria a config se não existir pra essa instância, ou atualiza a existente
// (mantém o mesmo Id/InboxId ao atualizar).
func (r *chatwootRepository) Upsert(cfg *chatwoot_model.ChatwootConfig) error {
	existing, err := r.GetByInstanceId(cfg.InstanceId)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return r.db.Create(cfg).Error
	}

	cfg.Id = existing.Id
	// autoCreateTime só se aplica no INSERT — Save() reescreve TODAS as colunas,
	// entao sem isso um cfg construido do zero (SetConfig nao seta CreatedAt) zera
	// a data de criacao original a cada update.
	cfg.CreatedAt = existing.CreatedAt
	if cfg.InboxId == "" {
		cfg.InboxId = existing.InboxId
	}
	if cfg.QrConversationId == "" {
		cfg.QrConversationId = existing.QrConversationId
	}
	return r.db.Save(cfg).Error
}

func (r *chatwootRepository) Delete(instanceId string) error {
	return r.db.Where("instance_id = ?", instanceId).Delete(&chatwoot_model.ChatwootConfig{}).Error
}

func (r *chatwootRepository) ClearQrConversation(instanceId string) error {
	return r.db.Model(&chatwoot_model.ChatwootConfig{}).Where("instance_id = ?", instanceId).Update("qr_conversation_id", "").Error
}
