package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/agentconfig"
)

type AgentSettingsRegistry interface {
	Get(context.Context, string) (agentconfig.Settings, error)
	Update(context.Context, string, agentconfig.UpdateInput) (agentconfig.Settings, error)
}

func registerAgentSettingsRoutes(api *gin.RouterGroup, registry AgentSettingsRegistry) {
	api.GET("/projects/:projectID/agent-settings", func(c *gin.Context) {
		if !requireAgentSettingsRegistry(c, registry) {
			return
		}
		settings, err := registry.Get(c.Request.Context(), c.Param("projectID"))
		if err != nil {
			writeAgentSettingsError(c, err)
			return
		}
		writeData(c, http.StatusOK, settings)
	})

	api.PUT("/projects/:projectID/agent-settings", func(c *gin.Context) {
		if !requireAgentSettingsRegistry(c, registry) {
			return
		}
		var request agentconfig.UpdateInput
		if err := c.ShouldBindJSON(&request); err != nil {
			writeError(c, http.StatusBadRequest, "invalid agent settings request")
			return
		}
		settings, err := registry.Update(c.Request.Context(), c.Param("projectID"), request)
		if err != nil {
			writeAgentSettingsError(c, err)
			return
		}
		writeData(c, http.StatusOK, settings)
	})
}

func requireAgentSettingsRegistry(c *gin.Context, registry AgentSettingsRegistry) bool {
	if registry == nil {
		writeError(c, http.StatusServiceUnavailable, "agent settings registry is unavailable")
		return false
	}
	return true
}

func writeAgentSettingsError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, agentconfig.ErrProjectNotFound):
		writeError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, agentconfig.ErrInvalidSettings):
		writeError(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, agentconfig.ErrVersionConflict):
		writeError(c, http.StatusConflict, "agent settings changed; reload before saving")
	default:
		writeError(c, http.StatusInternalServerError, "agent settings operation failed")
	}
}
