package schedule

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
)

// Handler expõe os agendamentos nas rotas autenticadas pela apikey da instância.
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

// List GET /instance/schedule/:instanceId?status=&limit=
func (h *Handler) List(c *gin.Context) {
	instanceId := c.Param("instanceId")
	if !sameInstance(c, instanceId) {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	list, err := h.svc.List(instanceId, c.Query("status"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"schedules": list})
}

// Create POST /instance/schedule/:instanceId
func (h *Handler) Create(c *gin.Context) {
	instanceId := c.Param("instanceId")
	if !sameInstance(c, instanceId) {
		return
	}
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	m, err := h.svc.Create(instanceId, &req)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"schedule": m})
}

// Delete DELETE /instance/schedule/:instanceId/:scheduleId
func (h *Handler) Delete(c *gin.Context) {
	instanceId := c.Param("instanceId")
	if !sameInstance(c, instanceId) {
		return
	}
	if err := h.svc.Delete(instanceId, c.Param("scheduleId")); err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success"})
}

// Pause POST /instance/schedule/:instanceId/:scheduleId/pause
func (h *Handler) Pause(c *gin.Context) {
	instanceId := c.Param("instanceId")
	if !sameInstance(c, instanceId) {
		return
	}
	m, err := h.svc.Pause(instanceId, c.Param("scheduleId"))
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"schedule": m})
}

// Resume POST /instance/schedule/:instanceId/:scheduleId/resume
func (h *Handler) Resume(c *gin.Context) {
	instanceId := c.Param("instanceId")
	if !sameInstance(c, instanceId) {
		return
	}
	m, err := h.svc.Resume(instanceId, c.Param("scheduleId"))
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"schedule": m})
}
