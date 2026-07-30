package chatwoot_handler

import (
	"errors"
	"net/http"

	chatwoot_service "github.com/EvolutionAPI/evolution-go/pkg/chatwoot/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ChatwootHandler interface {
	GetConfig(ctx *gin.Context)
	SetConfig(ctx *gin.Context)
	DeleteConfig(ctx *gin.Context)
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
