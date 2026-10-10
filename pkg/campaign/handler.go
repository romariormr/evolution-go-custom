package campaign

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
)

// Handler expõe as campanhas nas rotas autenticadas pela apikey da instância.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func sameInstance(c *gin.Context) (string, bool) {
	instanceId := c.Param("instanceId")
	v, ok := c.Get("instance")
	inst, _ := v.(*instance_model.Instance)
	if !ok || inst == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authorized"})
		return "", false
	}
	if inst.Id != instanceId {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "instanceId does not match authenticated instance"})
		return "", false
	}
	return instanceId, true
}

func respondErr(c *gin.Context, err error) {
	if errors.Is(err, ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

// List GET /instance/campaign/:instanceId
func (h *Handler) List(c *gin.Context) {
	id, ok := sameInstance(c)
	if !ok {
		return
	}
	list, err := h.svc.List(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"campaigns": list})
}

// Create POST /instance/campaign/:instanceId
func (h *Handler) Create(c *gin.Context) {
	id, ok := sameInstance(c)
	if !ok {
		return
	}
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	out, err := h.svc.Create(id, &req)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"campaign": out})
}

// Get GET /instance/campaign/:instanceId/:campaignId
func (h *Handler) Get(c *gin.Context) {
	id, ok := sameInstance(c)
	if !ok {
		return
	}
	out, err := h.svc.Get(id, c.Param("campaignId"))
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"campaign": out})
}

// Recipients GET /instance/campaign/:instanceId/:campaignId/recipients?status=&limit=&offset=
func (h *Handler) Recipients(c *gin.Context) {
	id, ok := sameInstance(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	list, err := h.svc.Recipients(id, c.Param("campaignId"), c.Query("status"), limit, offset)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"recipients": list})
}

func (h *Handler) action(fn func(instanceId, id string) (*Campaign, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := sameInstance(c)
		if !ok {
			return
		}
		out, err := fn(id, c.Param("campaignId"))
		if err != nil {
			respondErr(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"campaign": out})
	}
}

// Start/Pause/Cancel/Retry POST /instance/campaign/:instanceId/:campaignId/<ação>
func (h *Handler) Start() gin.HandlerFunc  { return h.action(h.svc.Start) }
func (h *Handler) Pause() gin.HandlerFunc  { return h.action(h.svc.Pause) }
func (h *Handler) Cancel() gin.HandlerFunc { return h.action(h.svc.Cancel) }
func (h *Handler) Retry() gin.HandlerFunc  { return h.action(h.svc.Retry) }

// Delete DELETE /instance/campaign/:instanceId/:campaignId
func (h *Handler) Delete(c *gin.Context) {
	id, ok := sameInstance(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(id, c.Param("campaignId")); err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success"})
}

// ListOptOuts GET /instance/optout/:instanceId
func (h *Handler) ListOptOuts(c *gin.Context) {
	id, ok := sameInstance(c)
	if !ok {
		return
	}
	list, err := h.svc.ListOptOuts(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"optOuts": list})
}

type optOutBody struct {
	Number string `json:"number"`
}

// AddOptOut POST /instance/optout/:instanceId {number}
func (h *Handler) AddOptOut(c *gin.Context) {
	id, ok := sameInstance(c)
	if !ok {
		return
	}
	var b optOutBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.AddOptOut(id, b.Number, "manual"); err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success"})
}

// RemoveOptOut DELETE /instance/optout/:instanceId/:number
func (h *Handler) RemoveOptOut(c *gin.Context) {
	id, ok := sameInstance(c)
	if !ok {
		return
	}
	if err := h.svc.RemoveOptOut(id, c.Param("number")); err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success"})
}
