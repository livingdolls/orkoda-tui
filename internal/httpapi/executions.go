package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

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
	api.GET("/executions/:executionID/changed-files", func(c *gin.Context) {
		if !requireExecutionRegistry(c, registry) {
			return
		}
		checkpoint, err := latestCheckpoint(c, registry, c.Param("executionID"))
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		var files []string
		if err := json.Unmarshal(checkpoint.ChangedFilesJSON, &files); err != nil {
			writeExecutionError(c, err)
			return
		}
		writeData(c, http.StatusOK, files)
	})
	api.GET("/executions/:executionID/diff", func(c *gin.Context) {
		if !requireExecutionRegistry(c, registry) {
			return
		}
		checkpoint, err := latestCheckpoint(c, registry, c.Param("executionID"))
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		patch := checkpoint.PatchText
		if path := strings.TrimSpace(c.Query("path")); path != "" {
			patch = filterDiffByPath(patch, path)
		}
		cursor, limit, err := diffPage(c)
		if err != nil {
			writeError(c, http.StatusBadRequest, err.Error())
			return
		}
		lines := strings.Split(patch, "\n")
		if len(lines) == 1 && lines[0] == "" {
			lines = []string{}
		}
		if cursor > len(lines) {
			cursor = len(lines)
		}
		end := cursor + limit
		if end > len(lines) {
			end = len(lines)
		}
		writeData(c, http.StatusOK, gin.H{
			"execution_id":  checkpoint.ExecutionID,
			"checkpoint_id": checkpoint.ID,
			"checksum":      checkpoint.PatchChecksum,
			"lines":         lines[cursor:end],
			"cursor":        end,
			"has_more":      end < len(lines),
		})
	})
}

func latestCheckpoint(c *gin.Context, registry ExecutionRegistry, executionID string) (execution.Checkpoint, error) {
	items, err := registry.ListCheckpoints(c.Request.Context(), executionID)
	if err != nil {
		return execution.Checkpoint{}, err
	}
	if len(items) == 0 {
		return execution.Checkpoint{}, execution.ErrNotFound
	}
	return items[len(items)-1], nil
}

func diffPage(c *gin.Context) (int, int, error) {
	cursor, limit := 0, 400
	var err error
	if value := c.Query("cursor"); value != "" {
		cursor, err = strconv.Atoi(value)
		if err != nil || cursor < 0 {
			return 0, 0, errors.New("cursor must be a non-negative integer")
		}
	}
	if value := c.Query("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 2000 {
			return 0, 0, errors.New("limit must be between 1 and 2000")
		}
	}
	return cursor, limit, nil
}

func filterDiffByPath(patch, path string) string {
	path = strings.TrimPrefix(strings.TrimSpace(path), "./")
	sections := strings.Split(patch, "diff --git ")
	var matched []string
	for _, section := range sections {
		if section == "" {
			continue
		}
		if strings.Contains(section, " b/"+path) || strings.Contains(section, " a/"+path) {
			matched = append(matched, "diff --git "+section)
		}
	}
	return strings.Join(matched, "")
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
