package telemetry

import (
	"time"

	"github.com/gin-gonic/gin"
)

// NOTE (fork change): telemetry has been disabled in this fork.
//
// Upstream POSTed the accessed route of every request to an external endpoint
// (https://log.evolution-api.com/telemetry). To guarantee there is no outbound
// "phone-home" from a self-hosted instance, all functions below are no-ops.
// The public API (types, middleware, constructor) is preserved unchanged so
// existing callers keep compiling and behaving normally.

type TelemetryData struct {
	Route      string    `json:"route"`
	APIVersion string    `json:"apiVersion"`
	Timestamp  time.Time `json:"timestamp"`
}

type telemetryService struct{}

func (t *telemetryService) TelemetryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

type TelemetryService interface {
	TelemetryMiddleware() gin.HandlerFunc
}

// SendTelemetry is intentionally a no-op: no data leaves the instance.
func SendTelemetry(route string) {}

func NewTelemetryService() TelemetryService {
	return &telemetryService{}
}
