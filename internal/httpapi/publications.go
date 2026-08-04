package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/publication"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

type PublicationRegistry interface {
	GetByWorkflow(context.Context, string) (publication.Record, error)
}

func registerPublicationRoutes(
	api *gin.RouterGroup,
	publications PublicationRegistry,
	workflows WorkflowJobRegistry,
	repositories RepositoryMetadataRegistry,
	workspaces WorkspaceRegistry,
	remote publication.RemotePublisher,
) {
	api.GET("/jobs/:jobID/publications", func(c *gin.Context) {
		if publications == nil {
			writeError(c, http.StatusServiceUnavailable, "publication registry is unavailable")
			return
		}
		item, err := publications.GetByWorkflow(c.Request.Context(), c.Param("jobID"))
		if err != nil {
			writePublicationError(c, err)
			return
		}
		writeData(c, http.StatusOK, []publication.Record{item})
	})
	api.POST("/jobs/:jobID/publications/commit", func(c *gin.Context) {
		if workflows == nil {
			writeError(c, http.StatusServiceUnavailable, "workflow registry is unavailable")
			return
		}
		var request workflowActionRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			writeError(c, http.StatusBadRequest, "request body must contain expected_version")
			return
		}
		job, err := workflows.Transition(c.Request.Context(), c.Param("jobID"), workflowjob.TransitionInput{
			ExpectedVersion: request.ExpectedVersion,
			Action:          workflowjob.ActionPublish,
			Details:         request.Details,
		})
		if err != nil {
			writeWorkflowJobError(c, err)
			return
		}
		writeData(c, http.StatusAccepted, job)
	})
	registerRemotePublicationRoute(api, "/jobs/:jobID/publications/push", false, publications, workflows, repositories, workspaces, remote)
	registerRemotePublicationRoute(api, "/jobs/:jobID/publications/pull-request", true, publications, workflows, repositories, workspaces, remote)
}

func registerRemotePublicationRoute(
	api *gin.RouterGroup,
	path string,
	createPullRequest bool,
	publications PublicationRegistry,
	workflows WorkflowJobRegistry,
	repositories RepositoryMetadataRegistry,
	workspaces WorkspaceRegistry,
	remote publication.RemotePublisher,
) {
	api.POST(path, func(c *gin.Context) {
		if remote == nil || publications == nil || workflows == nil || repositories == nil || workspaces == nil {
			writeError(c, http.StatusServiceUnavailable, "remote publication is unavailable")
			return
		}
		job, err := workflows.Get(c.Request.Context(), c.Param("jobID"))
		if err != nil {
			writeWorkflowJobError(c, err)
			return
		}
		item, err := publications.GetByWorkflow(c.Request.Context(), job.ID)
		if err != nil {
			writePublicationError(c, err)
			return
		}
		repository, err := repositories.GetRepository(c.Request.Context(), job.RepositoryID)
		if err != nil {
			writeProjectError(c, err)
			return
		}
		workspaceItem, err := workspaces.GetByWorkflow(c.Request.Context(), job.ID)
		if err != nil {
			writeWorkspaceError(c, err)
			return
		}
		var request struct {
			Branch       string         `json:"branch_name"`
			TargetBranch string         `json:"target_branch"`
			Title        string         `json:"title"`
			Body         string         `json:"body"`
			Account      string         `json:"account"`
			Details      map[string]any `json:"details"`
		}
		if c.Request.ContentLength > 0 {
			if err := c.ShouldBindJSON(&request); err != nil {
				writeError(c, http.StatusBadRequest, "request body must be valid JSON")
				return
			}
		}
		branch := request.Branch
		if branch == "" {
			branch = "orkoda/" + job.ID
		}
		result, err := remote.Publish(c.Request.Context(), publication.RemotePublishInput{
			RepositoryURL:     repository.RemoteURL,
			WorkspacePath:     workspaceItem.Path,
			Branch:            branch,
			TargetBranch:      firstNonEmpty(request.TargetBranch, job.BaseBranch),
			Title:             firstNonEmpty(request.Title, "Orkoda: "+job.ID),
			Body:              request.Body,
			Account:           request.Account,
			Draft:             true,
			CreatePullRequest: createPullRequest,
		})
		if err != nil {
			if errors.Is(err, publication.ErrRemoteUnavailable) {
				writeError(c, http.StatusServiceUnavailable, err.Error())
				return
			}
			writeError(c, http.StatusConflict, err.Error())
			return
		}
		writeData(c, http.StatusOK, gin.H{"publication": item, "remote": result})
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func writePublicationError(c *gin.Context, err error) {
	if errors.Is(err, publication.ErrNotFound) {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}
	writeError(c, http.StatusInternalServerError, "publication operation failed")
}
