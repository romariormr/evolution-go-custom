package community_handler

import (
	"net/http"

	community_service "github.com/EvolutionAPI/evolution-go/pkg/community/service"
	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
	"github.com/gin-gonic/gin"
)

type CommunityHandler interface {
	CreateCommunity(ctx *gin.Context)
	CommunityAdd(ctx *gin.Context)
	CommunityRemove(ctx *gin.Context)
	ListCommunities(ctx *gin.Context)
	CommunityInfo(ctx *gin.Context)
	CommunityParticipants(ctx *gin.Context)
}

type communityHandler struct {
	communityService community_service.CommunityService
}

// Create community
// @Summary Create community
// @Description Create community
// @Tags Community
// @Accept json
// @Produce json
// @Param message body community_service.CreateCommunityStruct true "Community data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /community/create [post]
func (c *communityHandler) CreateCommunity(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "instance not found"})
		return
	}

	var data *community_service.CreateCommunityStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if data.CommunityName == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "community name is required"})
		return
	}

	community, err := c.communityService.CreateCommunity(data, instance)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": community})
}

// Add participant to community
// @Summary Add participant to community
// @Description Add participant to community
// @Tags Community
// @Accept json
// @Produce json
// @Param message body community_service.AddParticipantStruct true "Participant data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /community/add [post]
func (c *communityHandler) CommunityAdd(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "instance not found"})
		return
	}

	var data *community_service.AddParticipantStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if data.CommunityJID == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "community jid is required"})
		return
	}

	if len(data.GroupJID) == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "group jid is required"})
		return
	}

	resp, err := c.communityService.CommunityAdd(data, instance)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": resp})
}

// Remove participant from community
// @Summary Remove participant from community
// @Description Remove participant from community
// @Tags Community
// @Accept json
// @Produce json
// @Param message body community_service.AddParticipantStruct true "Participant data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /community/remove [post]
func (c *communityHandler) CommunityRemove(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "instance not found"})
		return
	}

	var data *community_service.AddParticipantStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if data.CommunityJID == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "community jid is required"})
		return
	}

	if len(data.GroupJID) == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "group jid is required"})
		return
	}

	resp, err := c.communityService.CommunityRemove(data, instance)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": resp})
}

func instanceFrom(ctx *gin.Context) (*instance_model.Instance, bool) {
	instance, ok := ctx.MustGet("instance").(*instance_model.Instance)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "instance not found"})
	}
	return instance, ok
}

// List communities
// @Summary List communities
// @Description Lista as comunidades das quais a instância participa (nome, descrição, participantes, se é admin)
// @Tags Community
// @Produce json
// @Success 200 {object} gin.H "success"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /community/list [get]
func (c *communityHandler) ListCommunities(ctx *gin.Context) {
	instance, ok := instanceFrom(ctx)
	if !ok {
		return
	}
	list, err := c.communityService.ListCommunities(instance)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": list})
}

func (c *communityHandler) bindJID(ctx *gin.Context) (*community_service.CommunityJIDStruct, bool) {
	var data community_service.CommunityJIDStruct
	if err := ctx.ShouldBindBodyWithJSON(&data); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return nil, false
	}
	if data.CommunityJID == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "communityJid é obrigatório"})
		return nil, false
	}
	return &data, true
}

// Community info
// @Summary Community info
// @Description Detalhes da comunidade com os grupos vinculados e o grupo de avisos (announcementJid)
// @Tags Community
// @Accept json
// @Produce json
// @Param message body community_service.CommunityJIDStruct true "Community JID"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /community/info [post]
func (c *communityHandler) CommunityInfo(ctx *gin.Context) {
	instance, ok := instanceFrom(ctx)
	if !ok {
		return
	}
	data, ok := c.bindJID(ctx)
	if !ok {
		return
	}
	info, err := c.communityService.CommunityInfo(data, instance)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": info})
}

// Community participants
// @Summary Community participants
// @Description Participantes de todos os grupos da comunidade
// @Tags Community
// @Accept json
// @Produce json
// @Param message body community_service.CommunityJIDStruct true "Community JID"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /community/participants [post]
func (c *communityHandler) CommunityParticipants(ctx *gin.Context) {
	instance, ok := instanceFrom(ctx)
	if !ok {
		return
	}
	data, ok := c.bindJID(ctx)
	if !ok {
		return
	}
	list, err := c.communityService.CommunityParticipants(data, instance)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": list, "count": len(list)})
}

func NewCommunityHandler(
	communityService community_service.CommunityService,
) CommunityHandler {
	return &communityHandler{
		communityService: communityService,
	}
}
