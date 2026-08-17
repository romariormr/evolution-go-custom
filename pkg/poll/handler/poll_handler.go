package poll_handler

import (
	"encoding/json"
	"net/http"
	"strings"

	logger_wrapper "github.com/EvolutionAPI/evolution-go/pkg/logger"
	poll_model "github.com/EvolutionAPI/evolution-go/pkg/poll/model"
	poll_service "github.com/EvolutionAPI/evolution-go/pkg/poll/service"
	"github.com/gin-gonic/gin"
)

// Keep poll_model referenced so the package import is not dropped
// (swag reads Go source, not pre-processed, and needs the alias to be in scope).
var _ = poll_model.PollResults{}

type PollHandler struct {
	pollService   poll_service.PollService
	loggerWrapper *logger_wrapper.LoggerManager
}

// NewPollHandler cria handler usando PollService existente (evita dupla inicialização)
func NewPollHandler(pollService poll_service.PollService, loggerWrapper *logger_wrapper.LoggerManager) *PollHandler {
	return &PollHandler{
		pollService:   pollService,
		loggerWrapper: loggerWrapper,
	}
}

// GetPollResults retorna os resultados de uma enquete
// @Summary Get poll results
// @Description Retorna todos os votos de uma enquete específica
// @Tags Polls
// @Accept json
// @Produce json
// @Param pollMessageId path string true "ID da mensagem da enquete"
// @Success 200 {object} poll_model.PollResults
// @Failure 400 {object} gin.H
// @Failure 404 {object} gin.H
// @Failure 500 {object} gin.H
// @Router /polls/{pollMessageId}/results [get]
func (h *PollHandler) GetPollResults(c *gin.Context) {
	pollMessageID := c.Param("pollMessageId")

	// Pegar instance do contexto de autenticação
	instanceInterface, exists := c.Get("instance")
	if !exists {
		h.loggerWrapper.GetLogger("poll-handler").LogWarn("[POLL] Instance not found in context")
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Authentication required",
		})
		return
	}

	// Converter para struct Instance
	type Instance struct {
		Id string `json:"id"`
	}
	instanceBytes, _ := json.Marshal(instanceInterface)
	var instance Instance
	if err := json.Unmarshal(instanceBytes, &instance); err != nil {
		h.loggerWrapper.GetLogger("poll-handler").LogError("[POLL] Failed to parse instance: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get instance information",
		})
		return
	}

	instanceID := instance.Id

	// Validações de segurança
	if pollMessageID == "" {
		h.loggerWrapper.GetLogger("poll-handler").LogWarn("[POLL] Missing pollMessageId")
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "pollMessageId is required",
		})
		return
	}

	// Opções informadas pelo chamador para rotular os hashes dos votos, quando a
	// enquete não tem definição guardada (ex.: enviada antes desta versão).
	// Aceita repetido (?option=A&option=B) e separado por barra (?options=A|B).
	providedOptions := append([]string{}, c.QueryArray("option")...)
	if raw := c.Query("options"); raw != "" {
		for _, part := range strings.Split(raw, "|") {
			if p := strings.TrimSpace(part); p != "" {
				providedOptions = append(providedOptions, p)
			}
		}
	}

	h.loggerWrapper.GetLogger("poll-handler").LogInfo("[POLL] Fetching results for poll %s (instance: %s)", pollMessageID, instanceID)

	// Buscar resultados do banco
	results, err := h.pollService.GetPollResults(c.Request.Context(), pollMessageID, instanceID, providedOptions)
	if err != nil {
		h.loggerWrapper.GetLogger("poll-handler").LogError("[POLL] Error fetching results: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to fetch poll results",
		})
		return
	}

	// 404 só quando não há NADA para mostrar: nem voto, nem opção conhecida
	// (definição guardada ou informada no query).
	if results.TotalVoters == 0 && len(results.Options) == 0 {
		h.loggerWrapper.GetLogger("poll-handler").LogInfo("[POLL] Nothing to show for poll %s", pollMessageID)
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "No votes or options found for this poll",
			"message": "Esta enquete ainda não tem votos e não há opções registradas. Envie a enquete por esta versão ou passe ?options=A|B para rotular.",
		})
		return
	}

	h.loggerWrapper.GetLogger("poll-handler").LogInfo("[POLL] Returning poll %s: %d voter(s), %d option(s)", pollMessageID, results.TotalVoters, len(results.Options))
	c.JSON(http.StatusOK, results)
}
