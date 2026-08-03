package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/approval"
	"github.com/livingdolls/orkoda-tui/internal/execution"
	"github.com/livingdolls/orkoda-tui/internal/reviewer"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

type ApprovalRegistry interface {
	Decide(context.Context, string, approval.Kind, approval.DecideInput) (approval.Outcome, error)
	Get(context.Context, string) (approval.Decision, error)
	ListWorkflow(context.Context, string) ([]approval.Decision, error)
}

func registerApprovalRoutes(api *gin.RouterGroup, registry ApprovalRegistry) {
	api.GET("/jobs/:jobID/decisions", func(c *gin.Context) {
		if !requireApprovalRegistry(c, registry) {
			return
		}
		items, err := registry.ListWorkflow(c.Request.Context(), c.Param("jobID"))
		if err != nil {
			writeApprovalError(c, err)
			return
		}
		writeData(c, http.StatusOK, items)
	})
	api.GET("/decisions/:decisionID", func(c *gin.Context) {
		if !requireApprovalRegistry(c, registry) {
			return
		}
		item, err := registry.Get(c.Request.Context(), c.Param("decisionID"))
		if err != nil {
			writeApprovalError(c, err)
			return
		}
		writeData(c, http.StatusOK, item)
	})
	registerDecisionAction(api, registry, "/jobs/:jobID/approve", approval.KindApprove)
	registerDecisionAction(api, registry, "/jobs/:jobID/request-revision", approval.KindRequestRevision)
	registerDecisionAction(api, registry, "/jobs/:jobID/reject", approval.KindReject)
}

func registerDecisionAction(
	api *gin.RouterGroup,
	registry ApprovalRegistry,
	path string,
	kind approval.Kind,
) {
	api.POST(path, func(c *gin.Context) {
		if !requireApprovalRegistry(c, registry) {
			return
		}
		var request approval.DecideInput
		if err := c.ShouldBindJSON(&request); err != nil {
			writeError(c, http.StatusBadRequest, "request body must bind the current execution version, base commit, and patch checksum")
			return
		}
		outcome, err := registry.Decide(c.Request.Context(), c.Param("jobID"), kind, request)
		if err != nil {
			writeApprovalError(c, err)
			return
		}
		writeData(c, http.StatusOK, outcome)
	})
}

func requireApprovalRegistry(c *gin.Context, registry ApprovalRegistry) bool {
	if registry == nil {
		writeError(c, http.StatusServiceUnavailable, "approval registry is unavailable")
		return false
	}
	return true
}

func writeApprovalError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, approval.ErrNotFound), errors.Is(err, execution.ErrNotFound),
		errors.Is(err, reviewer.ErrNotFound), errors.Is(err, workflowjob.ErrNotFound):
		writeError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, approval.ErrInvalid):
		writeError(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, approval.ErrSnapshotConflict),
		errors.Is(err, approval.ErrBindingMismatch),
		errors.Is(err, approval.ErrReviewOverrideRequired),
		errors.Is(err, approval.ErrWorkflowNotAwaitingDecision),
		errors.Is(err, workflowjob.ErrVersionConflict),
		errors.Is(err, workflowjob.ErrInvalidTransition),
		errors.Is(err, workflowjob.ErrRevisionLimit):
		writeError(c, http.StatusConflict, err.Error())
	default:
		writeError(c, http.StatusInternalServerError, "approval operation failed")
	}
}
