package history

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
)

// Handler expõe o histórico nas rotas autenticadas pela apikey da instância.
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

func parseTime(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, errors.New("data inválida (use RFC3339, ex. 2026-10-10T09:00:00-03:00): " + s)
	}
	return &t, nil
}

// Search GET /instance/history/:instanceId?chat=&q=&type=&fromMe=&before=&after=&limit=
func (h *Handler) Search(c *gin.Context) {
	id, ok := sameInstance(c)
	if !ok {
		return
	}
	q := Query{Chat: c.Query("chat"), Q: c.Query("q"), Type: c.Query("type")}
	q.Limit, _ = strconv.Atoi(c.Query("limit"))
	if v := c.Query("fromMe"); v != "" {
		b := v == "true"
		q.FromMe = &b
	}
	var err error
	if q.Before, err = parseTime(c.Query("before")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if q.After, err = parseTime(c.Query("after")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	list, err := h.svc.Search(id, q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resp := gin.H{"messages": list}
	if len(list) == q.Limit && len(list) > 0 {
		resp["nextBefore"] = list[len(list)-1].Timestamp.Format(time.RFC3339Nano)
	}
	c.JSON(http.StatusOK, resp)
}

// Chats GET /instance/history/:instanceId/chats?limit=
func (h *Handler) Chats(c *gin.Context) {
	id, ok := sameInstance(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	list, err := h.svc.Chats(id, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"chats": list})
}

// Get GET /instance/history/:instanceId/message/:messageId
func (h *Handler) Get(c *gin.Context) {
	id, ok := sameInstance(c)
	if !ok {
		return
	}
	e, err := h.svc.Get(id, c.Param("messageId"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": e})
}
