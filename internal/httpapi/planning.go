package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/planningcontext"
	"github.com/livingdolls/orkoda-tui/internal/repositorysummary"
)

type RepositorySummaryRegistry interface {
	Generate(context.Context, string) (repositorysummary.Summary, error)
	Current(context.Context, string) (repositorysummary.Summary, error)
}

type PlanningContextRegistry interface {
	Normalize(context.Context, string) (planningcontext.Context, error)
	Current(context.Context, string) (planningcontext.Context, error)
}

func registerPlanningRoutes(
	api *gin.RouterGroup,
	summaries RepositorySummaryRegistry,
	contexts PlanningContextRegistry,
) {
	api.POST("/repositories/:repositoryID/summaries", func(c *gin.Context) {
		if summaries == nil {
			writeError(c, http.StatusServiceUnavailable, "repository summary service is unavailable")
			return
		}
		summary, err := summaries.Generate(c.Request.Context(), c.Param("repositoryID"))
		if err != nil {
			writeRepositorySummaryError(c, err)
			return
		}
		writeData(c, http.StatusCreated, summary)
	})

	api.GET("/repositories/:repositoryID/summaries/current", func(c *gin.Context) {
		if summaries == nil {
			writeError(c, http.StatusServiceUnavailable, "repository summary service is unavailable")
			return
		}
		summary, err := summaries.Current(c.Request.Context(), c.Param("repositoryID"))
		if err != nil {
			writeRepositorySummaryError(c, err)
			return
		}
		writeData(c, http.StatusOK, summary)
	})

	api.POST("/plans/:planID/normalize", func(c *gin.Context) {
		if contexts == nil {
			writeError(c, http.StatusServiceUnavailable, "planning context service is unavailable")
			return
		}
		planningContext, err := contexts.Normalize(c.Request.Context(), c.Param("planID"))
		if err != nil {
			writePlanningContextError(c, err)
			return
		}
		writeData(c, http.StatusCreated, planningContext)
	})

	api.GET("/plans/:planID/context", func(c *gin.Context) {
		if contexts == nil {
			writeError(c, http.StatusServiceUnavailable, "planning context service is unavailable")
			return
		}
		planningContext, err := contexts.Current(c.Request.Context(), c.Param("planID"))
		if err != nil {
			writePlanningContextError(c, err)
			return
		}
		writeData(c, http.StatusOK, planningContext)
	})
}

func writeRepositorySummaryError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, repositorysummary.ErrRepositoryNotFound):
		writeError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, repositorysummary.ErrNotFound):
		writeError(c, http.StatusNotFound, "repository summary has not been generated for the current HEAD")
	default:
		writeError(c, http.StatusInternalServerError, "repository summary operation failed")
	}
}

func writePlanningContextError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, planningcontext.ErrPlanNotFound):
		writeError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, planningcontext.ErrSummaryMissing):
		writeError(c, http.StatusConflict, "scan the current repository HEAD before normalizing the plan")
	case errors.Is(err, planningcontext.ErrNotFound):
		writeError(c, http.StatusNotFound, "planning context has not been generated for the current plan version and repository HEAD")
	default:
		writeError(c, http.StatusInternalServerError, "planning context operation failed")
	}
}
