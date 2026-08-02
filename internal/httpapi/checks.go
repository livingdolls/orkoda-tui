package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/checks"
)

type CheckRegistry interface {
	Get(context.Context, string) (checks.Run, error)
	ListWorkflow(context.Context, string) ([]checks.Run, error)
	ListSteps(context.Context, string) ([]checks.Step, error)
}

func registerCheckRoutes(api *gin.RouterGroup, registry CheckRegistry) {
	api.GET("/jobs/:jobID/checks", func(c *gin.Context) {
		if !requireCheckRegistry(c, registry) { return }
		items, err := registry.ListWorkflow(c.Request.Context(), c.Param("jobID"))
		if err != nil { writeCheckError(c, err); return }
		writeData(c, http.StatusOK, items)
	})
	api.GET("/checks/:checkID", func(c *gin.Context) {
		if !requireCheckRegistry(c, registry) { return }
		item, err := registry.Get(c.Request.Context(), c.Param("checkID"))
		if err != nil { writeCheckError(c, err); return }
		writeData(c, http.StatusOK, item)
	})
	api.GET("/checks/:checkID/steps", func(c *gin.Context) {
		if !requireCheckRegistry(c, registry) { return }
		items, err := registry.ListSteps(c.Request.Context(), c.Param("checkID"))
		if err != nil { writeCheckError(c, err); return }
		writeData(c, http.StatusOK, items)
	})
}

func requireCheckRegistry(c *gin.Context, registry CheckRegistry) bool {
	if registry == nil { writeError(c, http.StatusServiceUnavailable, "check registry is unavailable"); return false }
	return true
}

func writeCheckError(c *gin.Context, err error) {
	if errors.Is(err, checks.ErrNotFound) { writeError(c, http.StatusNotFound, err.Error()); return }
	if errors.Is(err, checks.ErrInvalid) { writeError(c, http.StatusBadRequest, err.Error()); return }
	writeError(c, http.StatusInternalServerError, "check operation failed")
}
