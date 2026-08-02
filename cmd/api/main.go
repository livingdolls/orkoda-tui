package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/livingdolls/orkoda-tui/internal/activity"
	"github.com/livingdolls/orkoda-tui/internal/agentconfig"
	"github.com/livingdolls/orkoda-tui/internal/config"
	"github.com/livingdolls/orkoda-tui/internal/database"
	"github.com/livingdolls/orkoda-tui/internal/eventbus"
	"github.com/livingdolls/orkoda-tui/internal/execution"
	"github.com/livingdolls/orkoda-tui/internal/gitrepo"
	"github.com/livingdolls/orkoda-tui/internal/httpapi"
	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
	"github.com/livingdolls/orkoda-tui/internal/llm"
	"github.com/livingdolls/orkoda-tui/internal/llm/openaicompat"
	"github.com/livingdolls/orkoda-tui/internal/planningagent"
	"github.com/livingdolls/orkoda-tui/internal/planningcontext"
	"github.com/livingdolls/orkoda-tui/internal/plans"
	"github.com/livingdolls/orkoda-tui/internal/projects"
	"github.com/livingdolls/orkoda-tui/internal/repositorysummary"
	"github.com/livingdolls/orkoda-tui/internal/scheduler"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
	"github.com/livingdolls/orkoda-tui/internal/workspace"
)

type componentResult struct {
	name string
	err  error
}

func main() {
	if err := run(); err != nil {
		slog.Error("daemon stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	runtimeCtx, stopRuntime := context.WithCancel(signalCtx)
	defer stopRuntime()

	db, err := database.Open(runtimeCtx, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := database.Migrate(runtimeCtx, db); err != nil {
		return err
	}
	logger.Info("sqlite ready", "path", cfg.DatabasePath)

	activityRepository := activity.NewRepository(db)
	liveEvents := eventbus.New()
	defer liveEvents.Close()
	activityRecorder, err := activity.NewRecorder(activityRepository, liveEvents)
	if err != nil {
		return err
	}

	projectRepository, err := projects.NewRepository(db, gitrepo.NewInspector())
	if err != nil {
		return err
	}
	agentSettingsRepository, err := agentconfig.NewRepository(db, activityRecorder)
	if err != nil {
		return err
	}
	planRepository, err := plans.NewRepository(db, activityRecorder)
	if err != nil {
		return err
	}
	summaryRepository, err := repositorysummary.NewRepository(
		db,
		repositorysummary.NewScanner(),
		activityRecorder,
	)
	if err != nil {
		return err
	}
	planningContextRepository, err := planningcontext.NewRepository(
		db,
		summaryRepository,
		activityRecorder,
	)
	if err != nil {
		return err
	}

	localPlanningProvider := planningagent.NewLocalFakeProvider()
	providerRegistry, err := llm.NewRegistry(localPlanningProvider)
	if err != nil {
		return err
	}
	defaultProvider := planningagent.LocalFakeProviderName
	defaultModel := planningagent.LocalFakeModelName
	if cfg.LLM.Provider != planningagent.LocalFakeProviderName {
		provider, err := openaicompat.New(openaicompat.Config{
			Name:         cfg.LLM.Provider,
			BaseURL:      cfg.LLM.BaseURL,
			APIKey:       cfg.LLM.APIKey,
			DefaultModel: cfg.LLM.Model,
			Headers:      cfg.LLM.Headers,
			Timeout:      cfg.LLM.Timeout,
			JSONMode:     openaicompat.JSONMode(cfg.LLM.JSONMode),
		})
		if err != nil {
			return err
		}
		if err := providerRegistry.Register(provider); err != nil {
			return err
		}
		defaultProvider = provider.Name()
		defaultModel = cfg.LLM.Model
	}
	providerCatalog := llm.NewCatalog(providerRegistry, defaultProvider)

	fallbacks := make([]llm.FallbackTarget, 0, len(cfg.LLM.Fallbacks))
	for _, fallback := range cfg.LLM.Fallbacks {
		fallbacks = append(fallbacks, llm.FallbackTarget{
			Provider: fallback.Provider,
			Model:    fallback.Model,
		})
	}
	policy := llm.ExecutionPolicy{
		AttemptTimeout: cfg.LLM.AttemptTimeout,
		MaxWallClock:   cfg.LLM.MaxWallClock,
		MaxAttempts:    cfg.LLM.MaxAttempts,
		InitialBackoff: cfg.LLM.InitialBackoff,
		MaxBackoff:     cfg.LLM.MaxBackoff,
		Jitter:         cfg.LLM.BackoffJitter,
		Fallbacks:      fallbacks,
		Budget: llm.TokenBudget{
			MaxInputTokens:  cfg.LLM.MaxInputTokens,
			MaxOutputTokens: cfg.LLM.MaxOutputTokens,
			MaxTotalTokens:  cfg.LLM.MaxTotalTokens,
		},
	}
	policyGateway, err := llm.NewPolicyGateway(
		providerRegistry,
		activityRecorder,
		policy,
		llm.ConservativeTokenEstimator{},
	)
	if err != nil {
		return err
	}
	llmGateway, err := llm.NewSafetyGateway(
		policyGateway,
		activityRecorder,
		llm.SafetyPolicy{
			RedactionMode:              llm.RedactionMode(cfg.LLM.RedactionMode),
			MaxRepairAttempts:          cfg.LLM.MaxRepairAttempts,
			MaxStructuredResponseBytes: cfg.LLM.MaxStructuredResponseBytes,
		},
		llm.NewStandardRedactor(),
		llm.JSONSchemaValidator{},
		llm.ConservativeTokenEstimator{},
	)
	if err != nil {
		return err
	}
	logger.Info(
		"LLM providers ready",
		"default_provider", defaultProvider,
		"default_model", defaultModel,
		"provider_count", len(providerCatalog.List()),
		"max_attempts", policy.MaxAttempts,
		"fallback_count", len(policy.Fallbacks),
		"max_total_tokens", policy.Budget.MaxTotalTokens,
		"redaction_mode", cfg.LLM.RedactionMode,
		"max_repair_attempts", cfg.LLM.MaxRepairAttempts,
		"max_structured_response_bytes", cfg.LLM.MaxStructuredResponseBytes,
	)

	planningAgentService, err := planningagent.NewService(
		db,
		planningContextRepository,
		planRepository,
		llmGateway,
		activityRecorder,
	)
	if err != nil {
		return err
	}

	queue := jobqueue.New(db)
	workflowJobRepository, err := workflowjob.NewRepository(db, queue, activityRecorder)
	if err != nil {
		return err
	}
	workspaceRoot, err := workspace.PrepareRoot(cfg.WorkspaceDir)
	if err != nil {
		return err
	}
	workspaceRepository, err := workspace.NewRepository(db, workspaceRoot)
	if err != nil {
		return err
	}
	executionRepository, err := execution.NewRepository(db)
	if err != nil {
		return err
	}
	workerID := fmt.Sprintf("local-daemon-%d", os.Getpid())
	prepareWorkspaceHandler, err := workspace.NewPrepareHandler(
		workflowJobRepository,
		workspaceRepository,
		workspace.NewWorktreeManager(),
		activityRecorder,
		workerID,
		cfg.WorkspaceLeaseTTL,
	)
	if err != nil {
		return err
	}
	executeHandler, err := execution.NewHandler(
		workflowJobRepository,
		workspaceRepository,
		agentSettingsRepository,
		executionRepository,
		execution.ScriptedRunner{},
		activityRecorder,
		workerID,
		cfg.WorkspaceLeaseTTL,
		defaultProvider,
		defaultModel,
	)
	if err != nil {
		return err
	}
	logger.Info(
		"workspace runtime ready",
		"root", workspaceRepository.Root(),
		"lease_ttl", cfg.WorkspaceLeaseTTL,
	)

	queueScheduler, err := scheduler.New(
		queue,
		scheduler.DefaultConfig(workerID),
		map[string]scheduler.Handler{
			"system.noop": func(_ context.Context, job jobqueue.Job) error {
				logger.Info("noop job handled", "job_id", job.ID)
				return nil
			},
			"workflow.prepare_workspace": prepareWorkspaceHandler.HandleDurable,
			"workflow.execute":           executeHandler.HandleDurable,
		},
		activityRecorder,
		logger,
	)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr: cfg.APIAddress(),
		Handler: httpapi.NewRouterWithServices(
			cfg.Environment,
			activityRepository,
			projectRepository,
			httpapi.RouterServices{
				Plans:               planRepository,
				RepositorySummaries: summaryRepository,
				PlanningContexts:    planningContextRepository,
				PlanningAgent:       planningAgentService,
				AgentSettings:       agentSettingsRepository,
				WorkflowJobs:        workflowJobRepository,
				Workspaces:          workspaceRepository,
				Executions:          executionRepository,
				LLMProviders:        providerCatalog,
				LLMPolicy:           llmGateway,
				DefaultLLMProvider:  defaultProvider,
				DefaultLLMModel:     defaultModel,
			},
		),
		ReadHeaderTimeout: 5 * cfg.ShutdownTimeout / 10,
	}

	results := make(chan componentResult, 2)
	go func() {
		logger.Info("daemon listening", "address", cfg.APIAddress(), "environment", cfg.Environment)
		results <- componentResult{name: "http", err: server.ListenAndServe()}
	}()
	go func() {
		logger.Info("queue scheduler started")
		results <- componentResult{name: "scheduler", err: queueScheduler.Run(runtimeCtx)}
	}()

	completed := 0
	var runErr error
	select {
	case <-signalCtx.Done():
		logger.Info("shutdown requested")
	case result := <-results:
		completed++
		runErr = componentError(result)
	}

	stopRuntime()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelShutdown()

	if err := server.Shutdown(shutdownCtx); err != nil && runErr == nil {
		runErr = fmt.Errorf("shutdown HTTP server: %w", err)
	}

	for completed < 2 {
		select {
		case result := <-results:
			completed++
			if err := componentError(result); err != nil && runErr == nil {
				runErr = err
			}
		case <-shutdownCtx.Done():
			if runErr == nil {
				runErr = fmt.Errorf("coordinated shutdown: %w", shutdownCtx.Err())
			}
			return runErr
		}
	}

	logger.Info("daemon shutdown complete")
	return runErr
}

func componentError(result componentResult) error {
	if result.err == nil {
		return nil
	}
	if result.name == "http" && errors.Is(result.err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("%s component stopped: %w", result.name, result.err)
}
