package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/workspace"
)

type WorkspaceRegistry interface {
	GetByWorkflow(context.Context, string) (workspace.Workspace, error)
	ListProject(context.Context, string) ([]workspace.Workspace, error)
}

type WorkspaceEditor interface {
	WorkspaceRegistry
	AcquireWrite(context.Context, string, string, time.Duration) (workspace.Lease, error)
	ReleaseWrite(context.Context, string, string, string, bool) (workspace.Workspace, error)
}

type WorkspaceSnapshotReader interface {
	InspectWrite(context.Context, string) (workspace.Workspace, error)
}

type WorkspaceMaintenance interface {
	Archive(context.Context, string) (workspace.Workspace, error)
	Cleanup(context.Context, time.Time) (workspace.CleanupReport, error)
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

	if editor, ok := registry.(WorkspaceEditor); ok {
		takeOver := func(c *gin.Context) {
			item, err := editor.GetByWorkflow(c.Request.Context(), c.Param("jobID"))
			if err != nil {
				writeWorkspaceError(c, err)
				return
			}
			owner := c.GetHeader("X-Client-ID")
			if owner == "" {
				owner = "manual:" + requestID(c)
			}
			lease, err := editor.AcquireWrite(c.Request.Context(), item.ID, owner, 30*time.Minute)
			if err != nil {
				writeWorkspaceError(c, err)
				return
			}
			writeData(c, http.StatusOK, gin.H{
				"workspace":     lease.Workspace,
				"session_token": lease.Token,
				"expires_at":    lease.Workspace.LeaseExpiresAt,
			})
		}
		api.POST("/jobs/:jobID/workspace/take-over", takeOver)
		api.POST("/jobs/:jobID/take-over", takeOver)
		api.POST("/workspaces/:workspaceID/release", func(c *gin.Context) {
			var request struct {
				Token   string `json:"session_token"`
				HeadSHA string `json:"head_sha"`
				Dirty   bool   `json:"dirty"`
			}
			if err := c.ShouldBindJSON(&request); err != nil {
				writeError(c, http.StatusBadRequest, "request body must contain session_token and head_sha")
				return
			}
			if snapshotReader, ok := registry.(WorkspaceSnapshotReader); ok {
				snapshot, snapshotErr := snapshotReader.InspectWrite(c.Request.Context(), c.Param("workspaceID"))
				if snapshotErr != nil {
					writeWorkspaceError(c, snapshotErr)
					return
				}
				request.HeadSHA = snapshot.HeadSHA
				request.Dirty = snapshot.Dirty
			}
			updated, err := editor.ReleaseWrite(c.Request.Context(), c.Param("workspaceID"), request.Token, request.HeadSHA, request.Dirty)
			if err != nil {
				writeWorkspaceError(c, err)
				return
			}
			writeData(c, http.StatusOK, updated)
		})
	}

	if maintenance, ok := registry.(WorkspaceMaintenance); ok {
		api.POST("/workspaces/:workspaceID/archive", func(c *gin.Context) {
			item, err := maintenance.Archive(c.Request.Context(), c.Param("workspaceID"))
			if err != nil {
				writeWorkspaceError(c, err)
				return
			}
			writeData(c, http.StatusOK, item)
		})
		api.POST("/workspaces/cleanup", func(c *gin.Context) {
			before := time.Now().UTC().Add(-7 * 24 * time.Hour)
			if value := c.Query("before"); value != "" {
				parsed, err := time.Parse(time.RFC3339, value)
				if err != nil {
					writeError(c, http.StatusBadRequest, "before must be an RFC3339 timestamp")
					return
				}
				before = parsed
			}
			report, err := maintenance.Cleanup(c.Request.Context(), before)
			if err != nil {
				writeWorkspaceError(c, err)
				return
			}
			writeData(c, http.StatusOK, report)
		})
	}
}

func writeWorkspaceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, workspace.ErrNotFound):
		writeError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, workspace.ErrInvalidWorkspace):
		writeError(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, workspace.ErrLeaseUnavailable), errors.Is(err, workspace.ErrLeaseLost):
		writeError(c, http.StatusConflict, err.Error())
	default:
		writeError(c, http.StatusInternalServerError, "workspace operation failed")
	}
}
