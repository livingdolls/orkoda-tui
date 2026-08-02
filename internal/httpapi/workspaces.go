package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/workspace"
)

type WorkspaceRegistry interface {
	GetByWorkflow(context.Context, string) (workspace.Workspace, error)
	ListProject(context.Context, string) ([]workspace.Workspace, error)
}

func registerWorkspaceRoutes(api *gin.RouterGroup, registry WorkspaceRegistry) {
	api.GET("/jobs/:jobID/workspace", func(c *gin.Context) {
		if registry == nil {
			writeError(c, http.StatusServiceUnavailable, "workspace repository is unavailable")
			return
		}
		item, err := registry.GetByWorkflow(c.Request.Context(), c.Param("jobID"))
		if err != nil {
			writeWorkspaceError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": item})
	})

	api.GET("/projects/:projectID/workspaces", func(c *gin.Context) {
		if registry == nil {
			writeError(c, http.StatusServiceUnavailable, "workspace repository is unavailable")
			return
		}
		items, err := registry.ListProject(c.Request.Context(), c.Param("projectID"))
		if err != nil {
			writeWorkspaceError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": items})
	})
}

func writeWorkspaceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, workspace.ErrNotFound):
		writeError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, workspace.ErrInvalidWorkspace):
		writeError(c, http.StatusBadRequest, err.Error())
	default:
		writeError(c, http.StatusInternalServerError, "workspace operation failed")
	}
}
