package autoreply

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
)

// Handler expõe as regras nas rotas autenticadas pela apikey da instância.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func sameInstance(c *gin.Context, instanceId string) bool {
	v, ok := c.Get("instance")
	inst, _ := v.(*instance_model.Instance)
	if !ok || inst == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authorized"})
		return false
	}
	if inst.Id != instanceId {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "instanceId does not match authenticated instance"})
		return false
	}
	return true
}

func respondErr(c *gin.Context, err error) {
	if errors.Is(err, ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

// List GET /instance/autoreply/:instanceId
func (h *Handler) List(c *gin.Context) {
	instanceId := c.Param("instanceId")
	if !sameInstance(c, instanceId) {
		return
	}
	list, err := h.svc.List(instanceId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rules": list})
}

// Create POST /instance/autoreply/:instanceId
func (h *Handler) Create(c *gin.Context) {
	instanceId := c.Param("instanceId")
	if !sameInstance(c, instanceId) {
		return
	}
	var r Rule
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	out, err := h.svc.Create(instanceId, &r)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"rule": out})
}

// Update PUT /instance/autoreply/:instanceId/:ruleId
func (h *Handler) Update(c *gin.Context) {
	instanceId := c.Param("instanceId")
	if !sameInstance(c, instanceId) {
		return
	}
	var r Rule
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	out, err := h.svc.Update(instanceId, c.Param("ruleId"), &r)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"rule": out})
}

// Delete DELETE /instance/autoreply/:instanceId/:ruleId
func (h *Handler) Delete(c *gin.Context) {
	instanceId := c.Param("instanceId")
	if !sameInstance(c, instanceId) {
		return
	}
	if err := h.svc.Delete(instanceId, c.Param("ruleId")); err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success"})
}
