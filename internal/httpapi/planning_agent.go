package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/llm"
	"github.com/livingdolls/orkoda-tui/internal/planningagent"
	"github.com/livingdolls/orkoda-tui/internal/planningcontext"
	"github.com/livingdolls/orkoda-tui/internal/plans"
)

type PlanningAgentRegistry interface {
	Start(context.Context, string, string, string) (planningagent.Run, error)
	Current(context.Context, string) (planningagent.Run, error)
	Get(context.Context, string) (planningagent.Run, error)
	Answer(context.Context, string, []planningagent.AnswerInput) (planningagent.Run, error)
}

type startPlanningRunRequest struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type answerPlanningRunRequest struct {
	Answers []planningagent.AnswerInput `json:"answers"`
}

func registerPlanningAgentRoutes(
	api *gin.RouterGroup,
	service PlanningAgentRegistry,
	defaultProvider string,
	defaultModel string,
) {
	api.POST("/plans/:planID/planning-runs", func(c *gin.Context) {
		if service == nil {
			writeError(c, http.StatusServiceUnavailable, "planning agent service is unavailable")
			return
		}
		var request startPlanningRunRequest
		if c.Request.ContentLength > 0 {
			if err := c.ShouldBindJSON(&request); err != nil {
				writeError(c, http.StatusBadRequest, "invalid planning run request")
				return
			}
		}
		if strings.TrimSpace(request.Provider) == "" {
			request.Provider = defaultProvider
		}
		if strings.TrimSpace(request.Model) == "" {
			request.Model = defaultModel
		}
		run, err := service.Start(
			c.Request.Context(),
			c.Param("planID"),
			request.Provider,
			request.Model,
		)
		if err != nil {
			writePlanningAgentError(c, err)
			return
		}
		writeData(c, http.StatusCreated, run)
	})

	api.GET("/plans/:planID/planning-runs/current", func(c *gin.Context) {
		if service == nil {
			writeError(c, http.StatusServiceUnavailable, "planning agent service is unavailable")
			return
		}
		run, err := service.Current(c.Request.Context(), c.Param("planID"))
		if err != nil {
			writePlanningAgentError(c, err)
			return
		}
		writeData(c, http.StatusOK, run)
	})

	api.GET("/planning-runs/:runID", func(c *gin.Context) {
		if service == nil {
			writeError(c, http.StatusServiceUnavailable, "planning agent service is unavailable")
			return
		}
		run, err := service.Get(c.Request.Context(), c.Param("runID"))
		if err != nil {
			writePlanningAgentError(c, err)
			return
		}
		writeData(c, http.StatusOK, run)
	})

	api.POST("/planning-runs/:runID/answers", func(c *gin.Context) {
		if service == nil {
			writeError(c, http.StatusServiceUnavailable, "planning agent service is unavailable")
			return
		}
		var request answerPlanningRunRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			writeError(c, http.StatusBadRequest, "invalid planning answer request")
			return
		}
		run, err := service.Answer(c.Request.Context(), c.Param("runID"), request.Answers)
		if err != nil {
			writePlanningAgentError(c, err)
			return
		}
		writeData(c, http.StatusCreated, run)
	})
}

func writePlanningAgentError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, planningagent.ErrRunNotFound), errors.Is(err, plans.ErrNotFound), errors.Is(err, planningcontext.ErrPlanNotFound):
		writeError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, planningagent.ErrActiveRun):
		writeError(c, http.StatusConflict, "the plan already has an active planning run")
	case errors.Is(err, planningagent.ErrRunNotAwaitingInput):
		writeError(c, http.StatusConflict, err.Error())
	case errors.Is(err, planningagent.ErrStaleRun):
		writeError(c, http.StatusConflict, "the plan version or repository context changed; start a new planning run")
	case errors.Is(err, planningagent.ErrInvalidAnswers):
		writeError(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, planningcontext.ErrSummaryMissing):
		writeError(c, http.StatusConflict, "scan and normalize the current repository HEAD before starting the planning agent")
	case errors.Is(err, planningcontext.ErrNotFound):
		writeError(c, http.StatusConflict, "normalize the current plan before starting the planning agent")
	default:
		if providerError, ok := llm.AsProviderError(err); ok {
			status := http.StatusBadGateway
			switch providerError.Code {
			case llm.ErrorAuthentication:
				status = http.StatusUnauthorized
			case llm.ErrorRateLimited:
				status = http.StatusTooManyRequests
			case llm.ErrorCancelled, llm.ErrorTimeout:
				status = http.StatusRequestTimeout
			case llm.ErrorInvalidRequest,
				llm.ErrorInvalidResponse,
				llm.ErrorContextLength,
				llm.ErrorBudgetExceeded,
				llm.ErrorStructuredOutputInvalid,
				llm.ErrorStructuredOutputTooLarge,
				llm.ErrorRedactionFailed:
				status = http.StatusUnprocessableEntity
			}
			writeError(c, status, providerError.Message)
			return
		}
		writeError(c, http.StatusInternalServerError, "planning agent operation failed")
	}
}
