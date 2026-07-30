package chatwoot_service

import (
	"fmt"

	chatwoot_client "github.com/EvolutionAPI/evolution-go/pkg/chatwoot/client"
	chatwoot_model "github.com/EvolutionAPI/evolution-go/pkg/chatwoot/model"
	chatwoot_repository "github.com/EvolutionAPI/evolution-go/pkg/chatwoot/repository"
	instance_repository "github.com/EvolutionAPI/evolution-go/pkg/instance/repository"
	"github.com/gomessguii/logger"
)

// SetConfigStruct é o payload aceito em POST /instance/chatwoot/:instanceId.
// Campos que o usuário não informa (InboxId) ficam fora daqui de propósito.
type SetConfigStruct struct {
	Enabled                 bool   `json:"enabled"`
	Url                     string `json:"url"`
	AccountId               string `json:"accountId"`
	Token                   string `json:"token"`
	SignMsg                 bool   `json:"signMsg"`
	NameInbox               string `json:"nameInbox"`
	Organization            string `json:"organization"`
	Logo                    string `json:"logo"`
	ConversationPending     bool   `json:"conversationPending"`
	ReopenConversation      bool   `json:"reopenConversation"`
	ImportContacts          bool   `json:"importContacts"`
	ImportMessages          bool   `json:"importMessages"`
	DaysLimitImportMessages int    `json:"daysLimitImportMessages"`
	AutoCreate              bool   `json:"autoCreate"`
}

type ChatwootService interface {
	GetConfig(instanceId string) (*chatwoot_model.ChatwootConfig, error)
	// SetConfig salva a config e, se AutoCreate=true e ainda não existe inbox, tenta
	// criá-la no Chatwoot. Falha na criação da inbox NÃO impede o save da config —
	// erro vem em warning separado pro chamador decidir o que mostrar.
	SetConfig(instanceId string, input SetConfigStruct) (cfg *chatwoot_model.ChatwootConfig, inboxWarning string, err error)
	DeleteConfig(instanceId string) error
}

type chatwootService struct {
	repo         chatwoot_repository.ChatwootRepository
	instanceRepo instance_repository.InstanceRepository
	client       *chatwoot_client.Client
}

func NewChatwootService(repo chatwoot_repository.ChatwootRepository, instanceRepo instance_repository.InstanceRepository) ChatwootService {
	return &chatwootService{
		repo:         repo,
		instanceRepo: instanceRepo,
		client:       chatwoot_client.NewClient(),
	}
}

func (s *chatwootService) GetConfig(instanceId string) (*chatwoot_model.ChatwootConfig, error) {
	return s.repo.GetByInstanceId(instanceId)
}

func (s *chatwootService) SetConfig(instanceId string, input SetConfigStruct) (*chatwoot_model.ChatwootConfig, string, error) {
	if _, err := s.instanceRepo.GetInstanceByID(instanceId); err != nil {
		return nil, "", fmt.Errorf("instância não encontrada: %w", err)
	}

	if input.Enabled {
		if input.Url == "" || input.AccountId == "" || input.Token == "" {
			return nil, "", fmt.Errorf("url, accountId e token são obrigatórios quando enabled=true")
		}
	}

	cfg := &chatwoot_model.ChatwootConfig{
		InstanceId:              instanceId,
		Enabled:                 input.Enabled,
		Url:                     input.Url,
		AccountId:               input.AccountId,
		Token:                   input.Token,
		SignMsg:                 input.SignMsg,
		NameInbox:               input.NameInbox,
		Organization:            input.Organization,
		Logo:                    input.Logo,
		ConversationPending:     input.ConversationPending,
		ReopenConversation:      input.ReopenConversation,
		ImportContacts:          input.ImportContacts,
		ImportMessages:          input.ImportMessages,
		DaysLimitImportMessages: input.DaysLimitImportMessages,
		AutoCreate:              input.AutoCreate,
	}

	if err := s.repo.Upsert(cfg); err != nil {
		return nil, "", err
	}

	var inboxWarning string
	if cfg.Enabled && cfg.AutoCreate && cfg.InboxId == "" {
		inboxName := cfg.NameInbox
		if inboxName == "" {
			inboxName = instanceId
		}
		inboxId, err := s.client.CreateInbox(cfg.Url, cfg.AccountId, cfg.Token, inboxName)
		if err != nil {
			logger.LogWarn("[%s] falha ao criar inbox no Chatwoot automaticamente: %v", instanceId, err)
			inboxWarning = fmt.Sprintf("config salva, mas não foi possível criar a inbox automaticamente no Chatwoot: %v", err)
		} else {
			cfg.InboxId = inboxId
			if err := s.repo.Upsert(cfg); err != nil {
				logger.LogWarn("[%s] inbox criada (id=%s) mas falha ao salvar inboxId: %v", instanceId, inboxId, err)
			}
		}
	}

	return cfg, inboxWarning, nil
}

func (s *chatwootService) DeleteConfig(instanceId string) error {
	return s.repo.Delete(instanceId)
}
