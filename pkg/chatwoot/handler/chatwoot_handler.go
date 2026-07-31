package chatwoot_handler

import (
	"errors"
	"fmt"
	"net/http"

	chatwoot_service "github.com/EvolutionAPI/evolution-go/pkg/chatwoot/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ChatwootHandler interface {
	GetConfig(ctx *gin.Context)
	SetConfig(ctx *gin.Context)
	DeleteConfig(ctx *gin.Context)
	ResetStatusConversation(ctx *gin.Context)
	Webhook(ctx *gin.Context)
}

type chatwootHandler struct {
	service chatwoot_service.ChatwootService
}

func NewChatwootHandler(service chatwoot_service.ChatwootService) ChatwootHandler {
	return &chatwootHandler{service: service}
}

// GetConfig retorna a config de Chatwoot da instância.
// @Summary Busca a config de Chatwoot de uma instância
// @Tags Chatwoot
// @Produce json
// @Param instanceId path string true "Instance ID"
// @Success 200 {object} chatwoot_model.ChatwootConfig
// @Failure 404 {object} gin.H "Config não encontrada"
// @Router /instance/chatwoot/{instanceId} [get]
func (h *chatwootHandler) GetConfig(ctx *gin.Context) {
	instanceId := ctx.Param("instanceId")
	if instanceId == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "instanceId is required"})
		return
	}

	cfg, err := h.service.GetConfig(instanceId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"error": "chatwoot config not found for this instance"})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, cfg)
}

// SetConfig cria ou atualiza a config de Chatwoot da instância.
// @Summary Cria/atualiza a config de Chatwoot de uma instância
// @Tags Chatwoot
// @Accept json
// @Produce json
// @Param instanceId path string true "Instance ID"
// @Param config body chatwoot_service.SetConfigStruct true "Config do Chatwoot"
// @Success 200 {object} chatwoot_model.ChatwootConfig
// @Failure 400 {object} gin.H "Erro de validação"
// @Router /instance/chatwoot/{instanceId} [post]
func (h *chatwootHandler) SetConfig(ctx *gin.Context) {
	instanceId := ctx.Param("instanceId")
	if instanceId == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "instanceId is required"})
		return
	}

	var input chatwoot_service.SetConfigStruct
	if err := ctx.ShouldBindJSON(&input); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	cfg, inboxWarning, err := h.service.SetConfig(instanceId, input)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response := gin.H{"config": cfg}
	if inboxWarning != "" {
		response["warning"] = inboxWarning
	}
	ctx.JSON(http.StatusOK, response)
}

// DeleteConfig remove a config de Chatwoot da instância (desativa a integração).
// @Summary Remove a config de Chatwoot de uma instância
// @Tags Chatwoot
// @Produce json
// @Param instanceId path string true "Instance ID"
// @Success 200 {object} gin.H
// @Router /instance/chatwoot/{instanceId} [delete]
func (h *chatwootHandler) DeleteConfig(ctx *gin.Context) {
	instanceId := ctx.Param("instanceId")
	if instanceId == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "instanceId is required"})
		return
	}

	if err := h.service.DeleteConfig(instanceId); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "chatwoot config removed"})
}

// ResetStatusConversation esquece a conversa de status cacheada (QR code / aviso de
// conexão) dessa instância, sem mexer no resto da config — usar se ela ficou
// associada ao contato errado no Chatwoot. Próxima notificação cria uma nova.
// @Summary Reseta a conversa de status (QR/conexão) de uma instância
// @Tags Chatwoot
// @Produce json
// @Param instanceId path string true "Instance ID"
// @Success 200 {object} gin.H
// @Router /instance/chatwoot/{instanceId}/reset-status [post]
func (h *chatwootHandler) ResetStatusConversation(ctx *gin.Context) {
	instanceId := ctx.Param("instanceId")
	if instanceId == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "instanceId is required"})
		return
	}

	if err := h.service.ResetStatusConversation(instanceId); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "status conversation reset"})
}

type chatwootWebhookPayload struct {
	Event        string `json:"event"`
	MessageType  string `json:"message_type"`
	Private      bool   `json:"private"`
	Content      string `json:"content"`
	Conversation struct {
		Id int `json:"id"`
	} `json:"conversation"`
	Sender struct {
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"sender"`
	Attachments []struct {
		DataUrl  string `json:"data_url"`
		FileType string `json:"file_type"`
	} `json:"attachments"`
}

// Webhook recebe os eventos do Chatwoot (configurado na inbox: Settings ->
// Configuration -> Webhook URL). Rota PÚBLICA, sem o middleware de apikey —
// o Chatwoot não manda esse header. Sempre responde 200 (mesmo em no-op) pra
// não entrar em retry-loop do lado do Chatwoot.
//
// ⚠️ Sem segredo compartilhado: qualquer um que souber essa URL pode postar
// um payload fingindo ser o Chatwoot e mandar mensagem pelo WhatsApp da
// instância. Recomendado restringir por IP (proxy/firewall) até ter um
// segredo/HMAC validado aqui.
// @Summary Recebe webhook do Chatwoot (resposta do agente)
// @Tags Chatwoot
// @Accept json
// @Produce json
// @Param instanceId path string true "Instance ID"
// @Success 200 {object} gin.H
// @Router /instance/chatwoot/webhook/{instanceId} [post]
func (h *chatwootHandler) Webhook(ctx *gin.Context) {
	instanceId := ctx.Param("instanceId")
	if instanceId == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "instanceId is required"})
		return
	}

	var payload chatwootWebhookPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		ctx.JSON(http.StatusOK, gin.H{"message": "ignored (invalid payload)"})
		return
	}

	if payload.Event != "message_created" || payload.MessageType != "outgoing" || payload.Private || payload.Sender.Type != "user" {
		ctx.JSON(http.StatusOK, gin.H{"message": "ignored"})
		return
	}

	attachments := make([]chatwoot_service.AgentReplyAttachment, 0, len(payload.Attachments))
	for _, a := range payload.Attachments {
		if a.DataUrl == "" {
			continue
		}
		attachments = append(attachments, chatwoot_service.AgentReplyAttachment{DataUrl: a.DataUrl, FileType: a.FileType})
	}

	conversationId := fmt.Sprintf("%d", payload.Conversation.Id)
	reply := chatwoot_service.AgentReplyStruct{
		Content:     payload.Content,
		SenderName:  payload.Sender.Name,
		Attachments: attachments,
	}
	if err := h.service.HandleAgentReply(instanceId, conversationId, reply); err != nil {
		ctx.JSON(http.StatusOK, gin.H{"message": "processed with error", "error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "ok"})
}
