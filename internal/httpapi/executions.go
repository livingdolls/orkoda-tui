package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/execution"
)

type ExecutionRegistry interface {
	Get(context.Context, string) (execution.Execution, error)
	ListWorkflow(context.Context, string) ([]execution.Execution, error)
	ListToolRuns(context.Context, string) ([]execution.ToolRun, error)
	ListCheckpoints(context.Context, string) ([]execution.Checkpoint, error)
	ListIterations(context.Context, string) ([]execution.Iteration, error)
}

func registerExecutionRoutes(api *gin.RouterGroup, registry ExecutionRegistry) {
	api.GET("/jobs/:jobID/executions", func(c *gin.Context) {
		if !requireExecutionRegistry(c, registry) {
			return
		}
		items, err := registry.ListWorkflow(c.Request.Context(), c.Param("jobID"))
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		writeData(c, http.StatusOK, items)
	})
	api.GET("/executions/:executionID", func(c *gin.Context) {
		if !requireExecutionRegistry(c, registry) {
			return
		}
		item, err := registry.Get(c.Request.Context(), c.Param("executionID"))
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		writeData(c, http.StatusOK, item)
	})
	api.GET("/executions/:executionID/iterations", func(c *gin.Context) {
		if !requireExecutionRegistry(c, registry) {
			return
		}
		items, err := registry.ListIterations(c.Request.Context(), c.Param("executionID"))
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		writeData(c, http.StatusOK, items)
	})
	api.GET("/executions/:executionID/tool-runs", func(c *gin.Context) {
		if !requireExecutionRegistry(c, registry) {
			return
		}
		items, err := registry.ListToolRuns(c.Request.Context(), c.Param("executionID"))
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		writeData(c, http.StatusOK, items)
	})
	api.GET("/executions/:executionID/checkpoints", func(c *gin.Context) {
		if !requireExecutionRegistry(c, registry) {
			return
		}
		items, err := registry.ListCheckpoints(c.Request.Context(), c.Param("executionID"))
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		writeData(c, http.StatusOK, items)
	})
}

func requireExecutionRegistry(c *gin.Context, registry ExecutionRegistry) bool {
	if registry == nil {
		writeError(c, http.StatusServiceUnavailable, "execution registry is unavailable")
		return false
	}
	return true
}

func writeExecutionError(c *gin.Context, err error) {
	if errors.Is(err, execution.ErrNotFound) {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, execution.ErrInvalid) {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	writeError(c, http.StatusInternalServerError, "execution operation failed")
}
