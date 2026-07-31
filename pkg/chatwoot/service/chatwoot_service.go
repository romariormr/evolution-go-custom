package chatwoot_service

import (
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	chatwoot_client "github.com/EvolutionAPI/evolution-go/pkg/chatwoot/client"
	chatwoot_model "github.com/EvolutionAPI/evolution-go/pkg/chatwoot/model"
	chatwoot_repository "github.com/EvolutionAPI/evolution-go/pkg/chatwoot/repository"
	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
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

	// ResetStatusConversation esquece a conversa de status cacheada (QrConversationId)
	// pra essa instância, sem mexer no resto da config. Próxima notificação cria
	// uma conversa nova do zero. Usar quando a conversa cacheada ficou associada
	// ao contato errado (ver ClearQrConversation).
	ResetStatusConversation(instanceId string) error

	// NotifyIncomingMessage repassa uma mensagem de texto recebida no WhatsApp pro
	// Chatwoot, criando contato/conversa do contato real (por JID) se ainda não
	// existir. No-op silencioso se a instância não tem Chatwoot habilitado.
	NotifyIncomingMessage(instanceId, jid, senderName, text string) error

	// NotifyIncomingMedia é a versão do NotifyIncomingMessage pra mídia (imagem,
	// áudio, vídeo, documento) — mediaType é "image"/"video"/"audio"/"document".
	NotifyIncomingMedia(instanceId, jid, senderName string, data []byte, mediaType, filename, caption string) error

	// HandleAgentReply processa o webhook do Chatwoot quando um agente responde
	// numa conversa — resolve o JID a partir da conversa e reenvia pro WhatsApp
	// via MessageSender. No-op silencioso se a conversa não corresponde a um
	// contato real conhecido (ex.: é a conversa de status QR/conexão).
	HandleAgentReply(instanceId, chatwootConversationId string, reply AgentReplyStruct) error

	// SetSender injeta o MessageSender depois da construção — necessário porque
	// send_service depende de whatsmeow_service, que depende de chatwoot_service
	// (pro fluxo de entrada), então não dá pra passar isso no construtor sem
	// criar um ciclo de inicialização em main.go.
	SetSender(sender MessageSender)
}

// MessageSender é a única coisa que chatwoot_service precisa do sendMessage
// pra devolver a resposta do agente pro WhatsApp — interface local (em vez de
// importar pkg/sendMessage/service direto) pra evitar ciclo de import:
// sendMessage -> whatsmeow -> chatwoot -> sendMessage.
type MessageSender interface {
	SendText(number, text string, instance *instance_model.Instance) error
	// SendMedia manda um arquivo — mediaType é "image"/"video"/"audio"/"document"
	// (mesma nomenclatura do endpoint /send/media já existente).
	SendMedia(number string, data []byte, mediaType, filename, caption string, instance *instance_model.Instance) error
}

// AgentReplyStruct é o que o handler do webhook do Chatwoot extrai do payload
// pra passar pro HandleAgentReply.
type AgentReplyStruct struct {
	Content     string
	SenderName  string
	Attachments []AgentReplyAttachment
}

type AgentReplyAttachment struct {
	DataUrl  string
	FileType string // "image", "audio", "video", "file" (nomenclatura do Chatwoot)
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
	repo           chatwoot_repository.ChatwootRepository
	contactMapRepo chatwoot_repository.ChatwootContactMapRepository
	instanceRepo   instance_repository.InstanceRepository
	client         *chatwoot_client.Client
	sender         MessageSender

	// contactLocks serializa find-or-create por (instanceId, jid) — sem isso,
	// duas mensagens quase simultâneas do mesmo contato podem criar dois
	// contatos/conversas duplicados no Chatwoot (bug real que o evolution-api
	// original já teve que corrigir: "Chatwoot contact duplication during
	// import"). Chave = instanceId+"|"+jid, valor = *sync.Mutex.
	contactLocks sync.Map
}

func NewChatwootService(repo chatwoot_repository.ChatwootRepository, contactMapRepo chatwoot_repository.ChatwootContactMapRepository, instanceRepo instance_repository.InstanceRepository) ChatwootService {
	return &chatwootService{
		repo:           repo,
		contactMapRepo: contactMapRepo,
		instanceRepo:   instanceRepo,
		client:         chatwoot_client.NewClient(),
	}
}

func (s *chatwootService) SetSender(sender MessageSender) {
	s.sender = sender
}

func (s *chatwootService) lockContact(instanceId, jid string) func() {
	key := instanceId + "|" + jid
	value, _ := s.contactLocks.LoadOrStore(key, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
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

func (s *chatwootService) ResetStatusConversation(instanceId string) error {
	return s.repo.ClearQrConversation(instanceId)
}

// ensureStatusConversation garante que existe a conversa (com o contato sintético)
// usada pra postar QR/status dessa instância, criando na primeira vez e cacheando
// o id pras próximas chamadas.
func (s *chatwootService) ensureStatusConversation(cfg *chatwoot_model.ChatwootConfig) (string, error) {
	if cfg.QrConversationId != "" {
		return cfg.QrConversationId, nil
	}

	contactId, sourceId, err := s.client.FindOrCreateContact(cfg.Url, cfg.AccountId, cfg.Token, cfg.InboxId, statusContactName, statusContactPhone(cfg.InboxId), fmt.Sprintf("evogo-qr-%s", cfg.InboxId))
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

	if err := s.client.SendMediaMessage(cfg.Url, cfg.AccountId, cfg.Token, conversationId, qrPNG, "qrcode.png", "qrgeneratedsuccesfully", "outgoing"); err != nil {
		logger.LogWarn("[%s] chatwoot: falha ao enviar QR code: %v", instanceId, err)
		return err
	}

	if err := s.client.SendTextMessage(cfg.Url, cfg.AccountId, cfg.Token, conversationId, "scanqr", "outgoing"); err != nil {
		logger.LogWarn("[%s] chatwoot: falha ao enviar aviso 'scanqr': %v", instanceId, err)
	}

	logger.LogInfo("[%s] chatwoot: QR code postado na conversa de status (conversation=%s)", instanceId, conversationId)
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

	if err := s.client.SendTextMessage(cfg.Url, cfg.AccountId, cfg.Token, conversationId, "cw.inbox.connected", "outgoing"); err != nil {
		logger.LogWarn("[%s] chatwoot: falha ao enviar aviso de conexão: %v", instanceId, err)
		return err
	}

	logger.LogInfo("[%s] chatwoot: aviso de conexão postado na conversa de status (conversation=%s)", instanceId, conversationId)
	return nil
}

// jidToPhone extrai um telefone E.164-ish a partir de um JID do whatsmeow
// (ex.: "558597731198:64@s.whatsapp.net" -> "+558597731198"). Usa o mesmo
// identifier (JID completo) que a integração Node/Baileys já usa nessa
// mesma conta Chatwoot, pra ficar consistente.
func jidToPhone(jid string) string {
	number := jid
	if at := strings.Index(number, "@"); at != -1 {
		number = number[:at]
	}
	if colon := strings.Index(number, ":"); colon != -1 {
		number = number[:colon]
	}
	return "+" + number
}

// ensureRealContactConversation garante contato+conversa de um contato REAL
// (por JID), reusando o cache em ChatwootContactMap. Travado por
// (instanceId, jid) pra evitar criar duplicado quando duas mensagens do
// mesmo contato chegam quase juntas.
func (s *chatwootService) ensureRealContactConversation(cfg *chatwoot_model.ChatwootConfig, jid, senderName string) (string, error) {
	unlock := s.lockContact(cfg.InstanceId, jid)
	defer unlock()

	if existing, err := s.contactMapRepo.GetByJid(cfg.InstanceId, jid); err == nil && existing.ChatwootConversationId != "" {
		return existing.ChatwootConversationId, nil
	}

	name := senderName
	phone := jidToPhone(jid)
	if name == "" {
		name = phone
	}

	contactId, sourceId, err := s.client.FindOrCreateContact(cfg.Url, cfg.AccountId, cfg.Token, cfg.InboxId, name, phone, jid)
	if err != nil {
		return "", fmt.Errorf("falha ao criar contato %s no chatwoot: %w", jid, err)
	}

	conversationId, err := s.client.CreateConversation(cfg.Url, cfg.AccountId, cfg.Token, cfg.InboxId, sourceId, contactId)
	if err != nil {
		return "", fmt.Errorf("falha ao criar conversa do contato %s no chatwoot: %w", jid, err)
	}

	mapping := &chatwoot_model.ChatwootContactMap{
		InstanceId:             cfg.InstanceId,
		Jid:                    jid,
		ChatwootContactId:      contactId,
		ChatwootConversationId: conversationId,
	}
	if err := s.contactMapRepo.Upsert(mapping); err != nil {
		logger.LogWarn("[%s] contato/conversa criados (jid=%s) mas falha ao cachear: %v", cfg.InstanceId, jid, err)
	}

	return conversationId, nil
}

func (s *chatwootService) NotifyIncomingMessage(instanceId, jid, senderName, text string) error {
	cfg, err := s.repo.GetByInstanceId(instanceId)
	if err != nil || !cfg.Enabled || cfg.InboxId == "" {
		return nil
	}
	if text == "" {
		return nil
	}

	conversationId, err := s.ensureRealContactConversation(cfg, jid, senderName)
	if err != nil {
		logger.LogWarn("[%s] chatwoot: %v", instanceId, err)
		return err
	}

	if err := s.client.SendTextMessage(cfg.Url, cfg.AccountId, cfg.Token, conversationId, text, "incoming"); err != nil {
		logger.LogWarn("[%s] chatwoot: falha ao enviar mensagem de %s: %v", instanceId, jid, err)
		return err
	}

	return nil
}

func (s *chatwootService) NotifyIncomingMedia(instanceId, jid, senderName string, data []byte, mediaType, filename, caption string) error {
	cfg, err := s.repo.GetByInstanceId(instanceId)
	if err != nil || !cfg.Enabled || cfg.InboxId == "" {
		return nil
	}
	if len(data) == 0 {
		return nil
	}

	conversationId, err := s.ensureRealContactConversation(cfg, jid, senderName)
	if err != nil {
		logger.LogWarn("[%s] chatwoot: %v", instanceId, err)
		return err
	}

	if err := s.client.SendMediaMessage(cfg.Url, cfg.AccountId, cfg.Token, conversationId, data, filename, caption, "incoming"); err != nil {
		logger.LogWarn("[%s] chatwoot: falha ao enviar mídia (%s) de %s: %v", instanceId, mediaType, jid, err)
		return err
	}

	return nil
}

// jidFromMapping extrai o número (sem @domínio) do JID cacheado, pro send_service.
func jidFromMapping(jid string) string {
	if at := strings.Index(jid, "@"); at != -1 {
		return jid[:at]
	}
	return jid
}

func (s *chatwootService) HandleAgentReply(instanceId, chatwootConversationId string, reply AgentReplyStruct) error {
	if s.sender == nil {
		return nil
	}
	if reply.Content == "" && len(reply.Attachments) == 0 {
		return nil
	}

	mapping, err := s.contactMapRepo.GetByConversationId(instanceId, chatwootConversationId)
	if err != nil {
		// Conversa não corresponde a nenhum contato real conhecido (ex.: é a
		// conversa de status QR/conexão, ou webhook de outra instância) — ignora.
		return nil
	}

	cfg, err := s.repo.GetByInstanceId(instanceId)
	if err != nil {
		return nil
	}

	instance, err := s.instanceRepo.GetInstanceByID(instanceId)
	if err != nil {
		return fmt.Errorf("instância não encontrada: %w", err)
	}

	number := jidFromMapping(mapping.Jid)

	content := reply.Content
	if cfg.SignMsg && reply.SenderName != "" && content != "" {
		content = fmt.Sprintf("*%s:*\n%s", reply.SenderName, content)
	}

	if len(reply.Attachments) > 0 {
		attachment := reply.Attachments[0]
		data, mediaType, filename, err := downloadChatwootAttachment(attachment)
		if err != nil {
			logger.LogWarn("[%s] chatwoot: falha ao baixar anexo do agente: %v", instanceId, err)
			return err
		}

		if err := s.sender.SendMedia(number, data, mediaType, filename, content, instance); err != nil {
			logger.LogWarn("[%s] chatwoot: falha ao reenviar mídia do agente pro WhatsApp (jid=%s): %v", instanceId, mapping.Jid, err)
			return err
		}

		logger.LogInfo("[%s] chatwoot: mídia do agente reenviada pro WhatsApp (jid=%s)", instanceId, mapping.Jid)
		return nil
	}

	if err := s.sender.SendText(number, content, instance); err != nil {
		logger.LogWarn("[%s] chatwoot: falha ao reenviar resposta do agente pro WhatsApp (jid=%s): %v", instanceId, mapping.Jid, err)
		return err
	}

	logger.LogInfo("[%s] chatwoot: resposta do agente reenviada pro WhatsApp (jid=%s)", instanceId, mapping.Jid)
	return nil
}

// downloadChatwootAttachment baixa o anexo que o agente enviou no Chatwoot (a
// data_url é pública, sem precisar de api_access_token) e traduz o file_type
// do Chatwoot pro "Type" que o send_service espera.
func downloadChatwootAttachment(attachment AgentReplyAttachment) (data []byte, mediaType, filename string, err error) {
	client := &http.Client{Timeout: 30 * time.Second}

	resp, err := client.Get(attachment.DataUrl)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", "", fmt.Errorf("chatwoot retornou %d ao baixar anexo", resp.StatusCode)
	}

	data, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", "", err
	}

	switch attachment.FileType {
	case "image":
		mediaType = "image"
	case "video":
		mediaType = "video"
	case "audio":
		mediaType = "audio"
	default:
		mediaType = "document"
	}

	filename = path.Base(attachment.DataUrl)
	if filename == "" || filename == "." || filename == "/" {
		filename = "arquivo"
	}

	return data, mediaType, filename, nil
}
