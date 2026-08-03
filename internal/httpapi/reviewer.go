package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/reviewer"
)

type ReviewRegistry interface {
	Get(context.Context, string) (reviewer.Run, error)
	ListWorkflow(context.Context, string) ([]reviewer.Run, error)
	ListIssues(context.Context, string) ([]reviewer.Issue, error)
}

func registerReviewRoutes(api *gin.RouterGroup, registry ReviewRegistry) {
	api.GET("/jobs/:jobID/reviews", func(c *gin.Context) {
		if !requireReviewRegistry(c, registry) {
			return
		}
		items, err := registry.ListWorkflow(c.Request.Context(), c.Param("jobID"))
		if err != nil {
			writeReviewError(c, err)
			return
		}
		writeData(c, http.StatusOK, items)
	})
	api.GET("/reviews/:reviewID", func(c *gin.Context) {
		if !requireReviewRegistry(c, registry) {
			return
		}
		item, err := registry.Get(c.Request.Context(), c.Param("reviewID"))
		if err != nil {
			writeReviewError(c, err)
			return
		}
		writeData(c, http.StatusOK, item)
	})
	api.GET("/reviews/:reviewID/issues", func(c *gin.Context) {
		if !requireReviewRegistry(c, registry) {
			return
		}
		items, err := registry.ListIssues(c.Request.Context(), c.Param("reviewID"))
		if err != nil {
			writeReviewError(c, err)
			return
		}
		writeData(c, http.StatusOK, items)
	})
}

func requireReviewRegistry(c *gin.Context, registry ReviewRegistry) bool {
	if registry == nil {
		writeError(c, http.StatusServiceUnavailable, "review registry is unavailable")
		return false
	}
	return true
}

func writeReviewError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, reviewer.ErrNotFound):
		writeError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, reviewer.ErrInvalid), errors.Is(err, reviewer.ErrSnapshotConflict):
		writeError(c, http.StatusBadRequest, err.Error())
	default:
		writeError(c, http.StatusInternalServerError, "review operation failed")
	}
}
