package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/activity"
	"github.com/livingdolls/orkoda-tui/internal/artifact"
	"github.com/livingdolls/orkoda-tui/internal/diagnostics"
	"github.com/livingdolls/orkoda-tui/internal/eventbus"
	"github.com/livingdolls/orkoda-tui/internal/observability"
	"github.com/livingdolls/orkoda-tui/internal/publication"
)

const protocolVersion = "v1"

type EventReader interface {
	ListAfter(context.Context, int64, int) ([]activity.Event, error)
	ListJobAfter(context.Context, string, int64, int) ([]activity.Event, error)
}

type EventSubscriber interface {
	Subscribe(int) (<-chan eventbus.Event, func())
}

type RouterServices struct {
	Plans               PlanRegistry
	Repositories        RepositoryMetadataRegistry
	RepositorySummaries RepositorySummaryRegistry
	PlanningContexts    PlanningContextRegistry
	PlanningAgent       PlanningAgentRegistry
	AgentSettings       AgentSettingsRegistry
	WorkflowJobs        WorkflowJobRegistry
	Workspaces          WorkspaceRegistry
	Executions          ExecutionRegistry
	Checks              CheckRegistry
	Reviews             ReviewRegistry
	Approvals           ApprovalRegistry
	LLMProviders        LLMProviderCatalog
	LLMPolicy           LLMPolicyReader
	DefaultLLMProvider  string
	DefaultLLMModel     string
	APIToken            string
	LiveEvents          EventSubscriber
	Idempotency         IdempotencyStore
	Publications        PublicationRegistry
	RemotePublisher     publication.RemotePublisher
	Diagnostics         diagnostics.Reader
	Metrics             *observability.Metrics
	Artifacts           artifact.Store
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
	router.Use(gin.Recovery())
	router.Use(requestIdentityMiddleware())
	router.Use(structuredRequestLogger(slog.Default()))

	router.GET("/health/live", func(c *gin.Context) {
		c.JSON(http.StatusOK, healthResponse{
			Status:          "ok",
			ProtocolVersion: protocolVersion,
			Timestamp:       time.Now().UTC().Format(time.RFC3339),
		})
	})

	api := router.Group("/api/v1")
	api.Use(requestLimitMiddleware(4 * 1024 * 1024))
	api.Use(idempotencyMiddleware(services.Idempotency))
	if services.Metrics != nil {
		api.Use(services.Metrics.Middleware())
	}
	if token := strings.TrimSpace(services.APIToken); token != "" {
		api.Use(apiTokenMiddleware(token))
	}
	api.GET("/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"service": "orkoda-local-daemon",
				"status":  "ready",
			},
			"meta": gin.H{
				"protocol_version": protocolVersion,
				"request_id":       requestID(c),
				"correlation_id":   correlationID(c),
			},
		})
	})
	api.GET("/metrics", func(c *gin.Context) {
		if services.Metrics == nil {
			writeError(c, http.StatusServiceUnavailable, "metrics are unavailable")
			return
		}
		writeData(c, http.StatusOK, services.Metrics.Snapshot())
	})
	api.GET("/diagnostics", func(c *gin.Context) {
		if services.Diagnostics == nil {
			writeError(c, http.StatusServiceUnavailable, "diagnostics are unavailable")
			return
		}
		item, err := services.Diagnostics.Read(c.Request.Context())
		if err != nil {
			writeError(c, http.StatusInternalServerError, "failed to read diagnostics")
			return
		}
		writeData(c, http.StatusOK, item)
	})
	api.POST("/diagnostics/bundle", func(c *gin.Context) {
		if services.Diagnostics == nil {
			writeError(c, http.StatusServiceUnavailable, "diagnostics are unavailable")
			return
		}
		key, err := services.Diagnostics.Bundle(c.Request.Context())
		if err != nil {
			writeError(c, http.StatusInternalServerError, "failed to create diagnostics bundle")
			return
		}
		writeData(c, http.StatusCreated, gin.H{"artifact_key": key})
	})
	registerArtifactRoutes(api, services.Artifacts)

	api.GET("/events", func(c *gin.Context) {
		serveEvents(c, events, services.LiveEvents, "", services.Metrics)
	})
	api.GET("/jobs/:jobID/events", func(c *gin.Context) {
		serveEvents(c, events, services.LiveEvents, c.Param("jobID"), services.Metrics)
	})
	registerProjectRoutes(api, projectRegistry)
	registerRepositoryRoutes(api, services.Repositories)
	registerAgentSettingsRoutes(api, services.AgentSettings)
	registerPlanRoutes(api, services.Plans)
	registerPlanningRoutes(api, services.RepositorySummaries, services.PlanningContexts)
	registerPlanningAgentRoutes(api, services.PlanningAgent, services.DefaultLLMProvider, services.DefaultLLMModel)
	registerWorkflowJobRoutes(api, services.WorkflowJobs)
	registerWorkspaceRoutes(api, services.Workspaces)
	registerExecutionRoutes(api, services.Executions)
	registerCheckRoutes(api, services.Checks)
	registerReviewRoutes(api, services.Reviews)
	registerReviewBoardRoutes(
		api, services.Plans, services.WorkflowJobs, services.AgentSettings,
		services.Executions, services.Checks, services.Reviews, services.Approvals,
	)
	registerApprovalRoutes(api, services.Approvals)
	registerPublicationRoutes(api, services.Publications, services.WorkflowJobs, services.Repositories, services.Workspaces, services.RemotePublisher)
	registerLLMRoutes(api, services.LLMProviders, services.LLMPolicy)

	return router
}

func requestLimitMiddleware(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}

func apiTokenMiddleware(expected string) gin.HandlerFunc {
	expected = strings.TrimSpace(expected)
	return func(c *gin.Context) {
		header := strings.Fields(c.GetHeader("Authorization"))
		provided := ""
		if len(header) == 2 && strings.EqualFold(header[0], "Bearer") {
			provided = header[1]
		}
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"message": "API bearer token is required"},
				"meta":  gin.H{"protocol_version": protocolVersion},
			})
			return
		}
		c.Next()
	}
}

func serveEvents(c *gin.Context, reader EventReader, subscriber EventSubscriber, jobID string, metrics *observability.Metrics) {
	if strings.Contains(strings.ToLower(c.GetHeader("Accept")), "text/event-stream") || c.Query("stream") == "true" {
		streamEvents(c, reader, subscriber, jobID, metrics)
		return
	}
	replayEvents(c, reader, jobID)
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

func streamEvents(c *gin.Context, reader EventReader, subscriber EventSubscriber, jobID string, metrics *observability.Metrics) {
	if reader == nil || subscriber == nil {
		writeError(c, http.StatusServiceUnavailable, "live activity stream is unavailable")
		return
	}
	afterSequence, limit, err := parseReplayQuery(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	// Subscribe before replaying so an event committed during the replay is
	// either observed live or filtered by its sequence, never lost.
	stream, unsubscribe := subscriber.Subscribe(32)
	defer unsubscribe()
	if metrics != nil {
		metrics.StreamOpened()
		defer metrics.StreamClosed()
	}
	var initial []activity.Event
	if jobID == "" {
		initial, err = reader.ListAfter(c.Request.Context(), afterSequence, limit)
	} else {
		initial, err = reader.ListJobAfter(c.Request.Context(), jobID, afterSequence, limit)
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "failed to replay activity events")
		return
	}
	if _, ok := c.Writer.(http.Flusher); !ok {
		writeError(c, http.StatusInternalServerError, "event stream flushing is unavailable")
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	flush := c.Writer.(http.Flusher)
	writeSSE := func(event replayEvent) error {
		body, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			return marshalErr
		}
		if _, writeErr := fmt.Fprintf(c.Writer, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.Type, body); writeErr != nil {
			return writeErr
		}
		flush.Flush()
		return nil
	}
	for _, event := range initial {
		if err := writeSSE(replayEvent{Sequence: event.Sequence, JobID: event.JobID, Type: event.Type, Payload: event.PayloadJSON, CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano)}); err != nil {
			return
		}
		afterSequence = event.Sequence
	}
	// Tell clients that the connection is established even when there are no
	// historical events to replay.
	if _, err := fmt.Fprint(c.Writer, ": connected\n\n"); err != nil {
		return
	}
	flush.Flush()
	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-keepAlive.C:
			if _, err := fmt.Fprint(c.Writer, ": keep-alive\n\n"); err != nil {
				return
			}
			flush.Flush()
		case event, open := <-stream:
			if !open {
				return
			}
			if jobID != "" && event.JobID != jobID {
				continue
			}
			if event.Sequence <= afterSequence {
				continue
			}
			if err := writeSSE(liveReplayEvent(event)); err != nil {
				return
			}
			afterSequence = event.Sequence
		}
	}
}

func liveReplayEvent(event eventbus.Event) replayEvent {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		payload = json.RawMessage(`{}`)
	}
	createdAt := event.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return replayEvent{
		Sequence:  event.Sequence,
		JobID:     event.JobID,
		Type:      event.Type,
		Payload:   payload,
		CreatedAt: createdAt.UTC().Format(time.RFC3339Nano),
	}
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
		"request_id": requestID(c),
	})
}

type requestIdentityKey string

const (
	requestIDKey     requestIdentityKey = "request_id"
	correlationIDKey requestIdentityKey = "correlation_id"
)

func requestIdentityMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if requestID == "" {
			requestID = newRequestID("req")
		}
		correlationID := strings.TrimSpace(c.GetHeader("X-Correlation-ID"))
		if correlationID == "" {
			correlationID = requestID
		}
		c.Set(string(requestIDKey), requestID)
		c.Set(string(correlationIDKey), correlationID)
		c.Header("X-Request-ID", requestID)
		c.Header("X-Correlation-ID", correlationID)
		c.Next()
	}
}

func structuredRequestLogger(logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		logger.Info("http request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"request_id", requestID(c),
			"correlation_id", correlationID(c),
			"duration_ms", time.Since(started).Milliseconds(),
		)
	}
}

func requestID(c *gin.Context) string {
	value, _ := c.Get(string(requestIDKey))
	result, _ := value.(string)
	return result
}

func correlationID(c *gin.Context) string {
	value, _ := c.Get(string(correlationIDKey))
	result, _ := value.(string)
	return result
}

func newRequestID(prefix string) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%x", prefix, raw[:])
}
