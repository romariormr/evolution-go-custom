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

	// NotifyQrCode posta o QR code recém-gerado na conversa de status da instância no
	// Chatwoot (cria contato+conversa na primeira vez). No-op silencioso se a
	// instância não tem Chatwoot habilitado/configurado — chamado a cada QR code
	// gerado, não pode travar nem logar como erro o caso comum de "não configurado".
	NotifyQrCode(instanceId string, qrPNG []byte, code string) error

	// NotifyConnected posta um aviso de conexão bem-sucedida na mesma conversa de
	// status. Mesmo no-op silencioso de NotifyQrCode quando não configurado.
	NotifyConnected(instanceId string) error
}

// Contato sintético usado só pra carregar a conversa de status (QR code, conectado)
// dentro do Chatwoot — não é um contato real do WhatsApp.
const statusContactName = "Gerador de QR"

// statusContactPhone precisa ser único por CONTA no Chatwoot (ele rejeita
// phone_number duplicado) e válido em E.164 (rejeita "+00073", por exemplo,
// com "Phone number should be in e164 format") — se duas instâncias
// apontarem pra mesma conta, cada uma precisa do seu próprio número
// sintético. Deriva do inboxId (que já é único dentro da conta) prefixado
// com um DDI+DDD plausível em vez de um valor fixo.
func statusContactPhone(inboxId string) string {
	return fmt.Sprintf("+1555%s", inboxId)
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

// ensureStatusConversation garante que existe a conversa (com o contato sintético)
// usada pra postar QR/status dessa instância, criando na primeira vez e cacheando
// o id pras próximas chamadas.
func (s *chatwootService) ensureStatusConversation(cfg *chatwoot_model.ChatwootConfig) (string, error) {
	if cfg.QrConversationId != "" {
		return cfg.QrConversationId, nil
	}

	contactId, sourceId, err := s.client.FindOrCreateContact(cfg.Url, cfg.AccountId, cfg.Token, cfg.InboxId, statusContactName, statusContactPhone(cfg.InboxId))
	if err != nil {
		return "", fmt.Errorf("falha ao criar contato de status no chatwoot: %w", err)
	}

	conversationId, err := s.client.CreateConversation(cfg.Url, cfg.AccountId, cfg.Token, cfg.InboxId, sourceId, contactId)
	if err != nil {
		return "", fmt.Errorf("falha ao criar conversa de status no chatwoot: %w", err)
	}

	cfg.QrConversationId = conversationId
	if err := s.repo.Upsert(cfg); err != nil {
		logger.LogWarn("[%s] conversa de status criada (id=%s) mas falha ao cachear: %v", cfg.InstanceId, conversationId, err)
	}

	return conversationId, nil
}

func (s *chatwootService) NotifyQrCode(instanceId string, qrPNG []byte, code string) error {
	cfg, err := s.repo.GetByInstanceId(instanceId)
	if err != nil || !cfg.Enabled || cfg.InboxId == "" {
		return nil
	}

	conversationId, err := s.ensureStatusConversation(cfg)
	if err != nil {
		logger.LogWarn("[%s] chatwoot: %v", instanceId, err)
		return err
	}

	if err := s.client.SendImageMessage(cfg.Url, cfg.AccountId, cfg.Token, conversationId, qrPNG, "qrcode.png", "qrgeneratedsuccesfully"); err != nil {
		logger.LogWarn("[%s] chatwoot: falha ao enviar QR code: %v", instanceId, err)
		return err
	}

	if err := s.client.SendTextMessage(cfg.Url, cfg.AccountId, cfg.Token, conversationId, "scanqr"); err != nil {
		logger.LogWarn("[%s] chatwoot: falha ao enviar aviso 'scanqr': %v", instanceId, err)
	}

	return nil
}

func (s *chatwootService) NotifyConnected(instanceId string) error {
	cfg, err := s.repo.GetByInstanceId(instanceId)
	if err != nil || !cfg.Enabled || cfg.InboxId == "" {
		return nil
	}

	conversationId, err := s.ensureStatusConversation(cfg)
	if err != nil {
		logger.LogWarn("[%s] chatwoot: %v", instanceId, err)
		return err
	}

	if err := s.client.SendTextMessage(cfg.Url, cfg.AccountId, cfg.Token, conversationId, "cw.inbox.connected"); err != nil {
		logger.LogWarn("[%s] chatwoot: falha ao enviar aviso de conexão: %v", instanceId, err)
		return err
	}

	return nil
}
