package auth_middleware

import (
	"net/http"

	access_repository "github.com/EvolutionAPI/evolution-go/pkg/access/repository"
	"github.com/EvolutionAPI/evolution-go/pkg/config"
	instance_service "github.com/EvolutionAPI/evolution-go/pkg/instance/service"
	"github.com/gin-gonic/gin"
)

type Middleware interface {
	Auth(ctx *gin.Context)
	AuthAdmin(ctx *gin.Context)
}

type middleware struct {
	config           *config.Config
	instanceService  instance_service.InstanceService
	accessRepository access_repository.AccessRepository
}

func (m middleware) Auth(ctx *gin.Context) {
	token := ctx.GetHeader("apikey")
	if token == "" {
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authorized"})
		return
	}

	instance, err := m.instanceService.GetInstanceByToken(token)
	if err != nil {
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authorized"})
		return
	}

	ctx.Set("instance", instance)

	ctx.Next()
}

func (m middleware) AuthAdmin(ctx *gin.Context) {
	token := ctx.GetHeader("apikey")
	if token == "" {
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authorized"})
		return
	}

	if token == m.config.GlobalApiKey {
		ctx.Next()
		return
	}

	// Não é a chave global — pode ser a chave de um Grupo (vê só as
	// instâncias vinculadas àquele grupo, não todas). Handlers que suportam
	// esse escopo reduzido leem ctx.Get("groupId") e filtram.
	if m.accessRepository != nil {
		if group, err := m.accessRepository.GetGroupByApiKey(token); err == nil {
			ctx.Set("groupId", group.Id)
			ctx.Next()
			return
		}
	}

	ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authorized"})
}

func NewMiddleware(config *config.Config, instanceService instance_service.InstanceService, accessRepository access_repository.AccessRepository) *middleware {
	return &middleware{config: config, instanceService: instanceService, accessRepository: accessRepository}
}
