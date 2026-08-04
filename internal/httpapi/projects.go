package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/gitrepo"
	"github.com/livingdolls/orkoda-tui/internal/projects"
)

type ProjectRegistry interface {
	Create(context.Context, string, string) (projects.Project, error)
	List(context.Context) ([]projects.Project, error)
	Get(context.Context, string) (projects.Project, error)
	Rename(context.Context, string, string) (projects.Project, error)
	Delete(context.Context, string) error
	Refresh(context.Context, string) (projects.Project, error)
}

type RepositoryMetadataRegistry interface {
	GetRepository(context.Context, string) (projects.RepositoryInfo, error)
	ListBranches(context.Context, string) ([]gitrepo.Branch, error)
	Trust(context.Context, string, string, map[string]any) (projects.RepositoryInfo, error)
}

type createProjectRequest struct {
	Name           string `json:"name"`
	RepositoryPath string `json:"repository_path"`
}

type renameProjectRequest struct {
	Name string `json:"name"`
}

type trustRepositoryRequest struct {
	Level        string         `json:"level"`
	IgnorePolicy map[string]any `json:"ignore_policy"`
}

func registerProjectRoutes(api *gin.RouterGroup, registry ProjectRegistry) {
	api.POST("/projects", func(c *gin.Context) {
		if !requireProjectRegistry(c, registry) {
			return
		}
		var request createProjectRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			writeError(c, http.StatusBadRequest, "request body must contain name and repository_path")
			return
		}
		project, err := registry.Create(c.Request.Context(), request.Name, request.RepositoryPath)
		if err != nil {
			writeProjectError(c, err)
			return
		}
		writeData(c, http.StatusCreated, project)
	})

	api.GET("/projects", func(c *gin.Context) {
		if !requireProjectRegistry(c, registry) {
			return
		}
		projectList, err := registry.List(c.Request.Context())
		if err != nil {
			writeProjectError(c, err)
			return
		}
		writeData(c, http.StatusOK, projectList)
	})

	api.GET("/projects/:projectID", func(c *gin.Context) {
		if !requireProjectRegistry(c, registry) {
			return
		}
		project, err := registry.Get(c.Request.Context(), c.Param("projectID"))
		if err != nil {
			writeProjectError(c, err)
			return
		}
		writeData(c, http.StatusOK, project)
	})

	api.PATCH("/projects/:projectID", func(c *gin.Context) {
		if !requireProjectRegistry(c, registry) {
			return
		}
		var request renameProjectRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			writeError(c, http.StatusBadRequest, "request body must contain name")
			return
		}
		project, err := registry.Rename(c.Request.Context(), c.Param("projectID"), request.Name)
		if err != nil {
			writeProjectError(c, err)
			return
		}
		writeData(c, http.StatusOK, project)
	})

	api.DELETE("/projects/:projectID", func(c *gin.Context) {
		if !requireProjectRegistry(c, registry) {
			return
		}
		if err := registry.Delete(c.Request.Context(), c.Param("projectID")); err != nil {
			writeProjectError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})

	api.POST("/projects/:projectID/refresh", func(c *gin.Context) {
		if !requireProjectRegistry(c, registry) {
			return
		}
		project, err := registry.Refresh(c.Request.Context(), c.Param("projectID"))
		if err != nil {
			writeProjectError(c, err)
			return
		}
		writeData(c, http.StatusOK, project)
	})
}

func registerRepositoryRoutes(api *gin.RouterGroup, registry RepositoryMetadataRegistry) {
	api.GET("/repositories/:repositoryID", func(c *gin.Context) {
		if registry == nil {
			writeError(c, http.StatusServiceUnavailable, "repository registry is unavailable")
			return
		}
		item, err := registry.GetRepository(c.Request.Context(), c.Param("repositoryID"))
		if err != nil {
			writeProjectError(c, err)
			return
		}
		writeData(c, http.StatusOK, item)
	})
	api.GET("/repositories/:repositoryID/branches", func(c *gin.Context) {
		if registry == nil {
			writeError(c, http.StatusServiceUnavailable, "repository registry is unavailable")
			return
		}
		branches, err := registry.ListBranches(c.Request.Context(), c.Param("repositoryID"))
		if err != nil {
			writeProjectError(c, err)
			return
		}
		writeData(c, http.StatusOK, branches)
	})
	api.GET("/repositories/:repositoryID/submodules", func(c *gin.Context) {
		if registry == nil {
			writeError(c, http.StatusServiceUnavailable, "repository registry is unavailable")
			return
		}
		item, err := registry.GetRepository(c.Request.Context(), c.Param("repositoryID"))
		if err != nil {
			writeProjectError(c, err)
			return
		}
		writeData(c, http.StatusOK, item.Submodules)
	})
	api.POST("/repositories/:repositoryID/trust", func(c *gin.Context) {
		if registry == nil {
			writeError(c, http.StatusServiceUnavailable, "repository registry is unavailable")
			return
		}
		var request trustRepositoryRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			writeError(c, http.StatusBadRequest, "request body must contain a trust level")
			return
		}
		item, err := registry.Trust(c.Request.Context(), c.Param("repositoryID"), request.Level, request.IgnorePolicy)
		if err != nil {
			writeProjectError(c, err)
			return
		}
		writeData(c, http.StatusOK, item)
	})
}

func requireProjectRegistry(c *gin.Context, registry ProjectRegistry) bool {
	if registry == nil {
		writeError(c, http.StatusServiceUnavailable, "project registry is unavailable")
		return false
	}
	return true
}

func writeProjectError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, projects.ErrInvalidProject):
		writeError(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, projects.ErrRepositoryAlreadyRegistered):
		writeError(c, http.StatusConflict, err.Error())
	case errors.Is(err, projects.ErrNotFound):
		writeError(c, http.StatusNotFound, err.Error())
	default:
		writeError(c, http.StatusInternalServerError, "project operation failed")
	}
}

func writeData(c *gin.Context, status int, data any) {
	c.JSON(status, gin.H{
		"data": data,
		"meta": gin.H{
			"protocol_version": protocolVersion,
			"request_id":       requestID(c),
			"correlation_id":   correlationID(c),
		},
	})
}
