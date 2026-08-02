package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/activity"
)

const protocolVersion = "v1"

type EventReader interface {
	ListAfter(context.Context, int64, int) ([]activity.Event, error)
	ListJobAfter(context.Context, string, int64, int) ([]activity.Event, error)
}

type RouterServices struct {
	Plans               PlanRegistry
	RepositorySummaries RepositorySummaryRegistry
	PlanningContexts    PlanningContextRegistry
	PlanningAgent       PlanningAgentRegistry
	AgentSettings       AgentSettingsRegistry
	WorkflowJobs        WorkflowJobRegistry
	Workspaces          WorkspaceRegistry
	Executions          ExecutionRegistry
	Checks              CheckRegistry
	LLMProviders        LLMProviderCatalog
	LLMPolicy           LLMPolicyReader
	DefaultLLMProvider  string
	DefaultLLMModel     string
}

type healthResponse struct {
	Status          string `json:"status"`
	ProtocolVersion string `json:"protocol_version"`
	Timestamp       string `json:"timestamp"`
}

type replayEvent struct {
	Sequence  int64           `json:"sequence"`
	JobID     string          `json:"job_id,omitempty"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt string          `json:"created_at"`
}

type replayMeta struct {
	LastSequence int64  `json:"last_sequence"`
	HasMore      bool   `json:"has_more"`
	Protocol     string `json:"protocol_version"`
}

func NewRouter(
	environment string,
	events EventReader,
	projectRegistry ProjectRegistry,
	planRegistries ...PlanRegistry,
) *gin.Engine {
	var planRegistry PlanRegistry
	if len(planRegistries) > 0 {
		planRegistry = planRegistries[0]
	}
	return NewRouterWithServices(environment, events, projectRegistry, RouterServices{Plans: planRegistry})
}

func NewRouterWithServices(
	environment string,
	events EventReader,
	projectRegistry ProjectRegistry,
	services RouterServices,
) *gin.Engine {
	if environment != "development" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	router.GET("/health/live", func(c *gin.Context) {
		c.JSON(http.StatusOK, healthResponse{
			Status:          "ok",
			ProtocolVersion: protocolVersion,
			Timestamp:       time.Now().UTC().Format(time.RFC3339),
		})
	})

	api := router.Group("/api/v1")
	api.GET("/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"service": "orkoda-local-daemon",
				"status":  "ready",
			},
			"meta": gin.H{
				"protocol_version": protocolVersion,
			},
		})
	})

	api.GET("/events", func(c *gin.Context) {
		replayEvents(c, events, "")
	})
	api.GET("/jobs/:jobID/events", func(c *gin.Context) {
		replayEvents(c, events, c.Param("jobID"))
	})
	registerProjectRoutes(api, projectRegistry)
	registerAgentSettingsRoutes(api, services.AgentSettings)
	registerPlanRoutes(api, services.Plans)
	registerPlanningRoutes(api, services.RepositorySummaries, services.PlanningContexts)
	registerPlanningAgentRoutes(api, services.PlanningAgent, services.DefaultLLMProvider, services.DefaultLLMModel)
	registerWorkflowJobRoutes(api, services.WorkflowJobs)
	registerWorkspaceRoutes(api, services.Workspaces)
	registerExecutionRoutes(api, services.Executions)
	registerCheckRoutes(api, services.Checks)
	registerLLMRoutes(api, services.LLMProviders, services.LLMPolicy)

	return router
}

func replayEvents(c *gin.Context, reader EventReader, jobID string) {
	if reader == nil {
		writeError(c, http.StatusServiceUnavailable, "activity repository is unavailable")
		return
	}

	afterSequence, limit, err := parseReplayQuery(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	fetchLimit := limit + 1
	var events []activity.Event
	if jobID == "" {
		events, err = reader.ListAfter(c.Request.Context(), afterSequence, fetchLimit)
	} else {
		events, err = reader.ListJobAfter(c.Request.Context(), jobID, afterSequence, fetchLimit)
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "failed to replay activity events")
		return
	}

	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}

	data := make([]replayEvent, 0, len(events))
	lastSequence := afterSequence
	for _, event := range events {
		data = append(data, replayEvent{
			Sequence:  event.Sequence,
			JobID:     event.JobID,
			Type:      event.Type,
			Payload:   event.PayloadJSON,
			CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
		lastSequence = event.Sequence
	}

	c.JSON(http.StatusOK, gin.H{
		"data": data,
		"meta": replayMeta{
			LastSequence: lastSequence,
			HasMore:      hasMore,
			Protocol:     protocolVersion,
		},
	})
}

func parseReplayQuery(c *gin.Context) (int64, int, error) {
	afterSequence := int64(0)
	if value := c.Query("after_sequence"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 0 {
			return 0, 0, fmt.Errorf("after_sequence must be a non-negative integer")
		}
		afterSequence = parsed
	}

	limit := activity.DefaultPageSize
	if value := c.Query("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > activity.MaxPageSize {
			return 0, 0, fmt.Errorf("limit must be between 1 and %d", activity.MaxPageSize)
		}
		limit = parsed
	}
	return afterSequence, limit, nil
}

func writeError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
		},
		"meta": gin.H{
			"protocol_version": protocolVersion,
		},
	})
}
