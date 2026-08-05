package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

type WorkflowJobRegistry interface {
	Create(context.Context, workflowjob.CreateInput) (workflowjob.Job, error)
	Get(context.Context, string) (workflowjob.Job, error)
	ListProject(context.Context, string) ([]workflowjob.Job, error)
	Transition(context.Context, string, workflowjob.TransitionInput) (workflowjob.Job, error)
	ListTransitions(context.Context, string) ([]workflowjob.Transition, error)
}

type createWorkflowJobRequest struct {
	PlanID               string                     `json:"plan_id"`
	RepositoryID         string                     `json:"repository_id"`
	BaseBranch           string                     `json:"base_branch"`
	AgentSettingsVersion int                        `json:"agent_settings_version"`
	Executor             workflowjob.AgentSelection `json:"executor"`
	Reviewer             workflowjob.AgentSelection `json:"reviewer"`
	Limits               workflowjob.Limits         `json:"limits"`
}

type workflowActionRequest struct {
	ExpectedVersion int            `json:"expected_version"`
	Details         map[string]any `json:"details"`
}

func registerWorkflowJobRoutes(api *gin.RouterGroup, registry WorkflowJobRegistry) {
	api.POST("/projects/:projectID/jobs", func(c *gin.Context) {
		if !requireWorkflowJobRegistry(c, registry) {
			return
		}
		var request createWorkflowJobRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			writeError(c, http.StatusBadRequest, "request body must contain plan_id and repository_id")
			return
		}
		job, err := registry.Create(c.Request.Context(), workflowjob.CreateInput{
			ProjectID:            c.Param("projectID"),
			PlanID:               request.PlanID,
			RepositoryID:         request.RepositoryID,
			BaseBranch:           request.BaseBranch,
			AgentSettingsVersion: request.AgentSettingsVersion,
			Executor:             request.Executor,
			Reviewer:             request.Reviewer,
			Limits:               request.Limits,
		})
		if err != nil {
			writeWorkflowJobError(c, err)
			return
		}
		writeData(c, http.StatusCreated, job)
	})

	api.GET("/projects/:projectID/jobs", func(c *gin.Context) {
		if !requireWorkflowJobRegistry(c, registry) {
			return
		}
		jobs, err := registry.ListProject(c.Request.Context(), c.Param("projectID"))
		if err != nil {
			writeWorkflowJobError(c, err)
			return
		}
		writeData(c, http.StatusOK, jobs)
	})

	// The kanban board needs only current workflow aggregates. Execution, checks,
	// review findings, artifacts, and diffs remain lazy-loaded when a card opens.
	api.GET("/projects/:projectID/board", func(c *gin.Context) {
		if !requireWorkflowJobRegistry(c, registry) {
			return
		}
		jobs, err := registry.ListProject(c.Request.Context(), c.Param("projectID"))
		if err != nil {
			writeWorkflowJobError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"jobs": jobs})
	})

	api.GET("/jobs/:jobID", func(c *gin.Context) {
		if !requireWorkflowJobRegistry(c, registry) {
			return
		}
		job, err := registry.Get(c.Request.Context(), c.Param("jobID"))
		if err != nil {
			writeWorkflowJobError(c, err)
			return
		}
		writeData(c, http.StatusOK, job)
	})

	api.GET("/jobs/:jobID/transitions", func(c *gin.Context) {
		if !requireWorkflowJobRegistry(c, registry) {
			return
		}
		transitions, err := registry.ListTransitions(c.Request.Context(), c.Param("jobID"))
		if err != nil {
			writeWorkflowJobError(c, err)
			return
		}
		writeData(c, http.StatusOK, transitions)
	})

	registerWorkflowAction(api, registry, "/jobs/:jobID/start", workflowjob.ActionStart)
	registerWorkflowAction(api, registry, "/jobs/:jobID/cancel", workflowjob.ActionCancel)
	registerWorkflowAction(api, registry, "/jobs/:jobID/retry", workflowjob.ActionRetry)
	registerWorkflowAction(api, registry, "/jobs/:jobID/publish", workflowjob.ActionPublish)
}

func registerWorkflowAction(
	api *gin.RouterGroup,
	registry WorkflowJobRegistry,
	path string,
	action workflowjob.Action,
) {
	api.POST(path, func(c *gin.Context) {
		if !requireWorkflowJobRegistry(c, registry) {
			return
		}
		var request workflowActionRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			writeError(c, http.StatusBadRequest, "request body must contain expected_version")
			return
		}
		job, err := registry.Transition(c.Request.Context(), c.Param("jobID"), workflowjob.TransitionInput{
			ExpectedVersion: request.ExpectedVersion,
			Action:          action,
			Details:         request.Details,
		})
		if err != nil {
			writeWorkflowJobError(c, err)
			return
		}
		writeData(c, http.StatusOK, job)
	})
}

func requireWorkflowJobRegistry(c *gin.Context, registry WorkflowJobRegistry) bool {
	if registry == nil {
		writeError(c, http.StatusServiceUnavailable, "workflow job registry is unavailable")
		return false
	}
	return true
}

func writeWorkflowJobError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, workflowjob.ErrNotFound), errors.Is(err, workflowjob.ErrProjectNotFound):
		writeError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, workflowjob.ErrInvalidJob):
		writeError(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, workflowjob.ErrPlanNotReady),
		errors.Is(err, workflowjob.ErrActiveJob),
		errors.Is(err, workflowjob.ErrVersionConflict),
		errors.Is(err, workflowjob.ErrInvalidTransition),
		errors.Is(err, workflowjob.ErrRevisionLimit):
		writeError(c, http.StatusConflict, err.Error())
	default:
		writeError(c, http.StatusInternalServerError, "workflow job operation failed")
	}
}
