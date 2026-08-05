package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/activity"
	"github.com/livingdolls/orkoda-tui/internal/agentconfig"
	"github.com/livingdolls/orkoda-tui/internal/approval"
	"github.com/livingdolls/orkoda-tui/internal/artifact"
	"github.com/livingdolls/orkoda-tui/internal/checks"
	"github.com/livingdolls/orkoda-tui/internal/config"
	"github.com/livingdolls/orkoda-tui/internal/credentials"
	"github.com/livingdolls/orkoda-tui/internal/database"
	"github.com/livingdolls/orkoda-tui/internal/diagnostics"
	"github.com/livingdolls/orkoda-tui/internal/eventbus"
	"github.com/livingdolls/orkoda-tui/internal/execution"
	"github.com/livingdolls/orkoda-tui/internal/gitrepo"
	"github.com/livingdolls/orkoda-tui/internal/httpapi"
	"github.com/livingdolls/orkoda-tui/internal/instance"
	"github.com/livingdolls/orkoda-tui/internal/jobqueue"
	"github.com/livingdolls/orkoda-tui/internal/llm"
	"github.com/livingdolls/orkoda-tui/internal/llm/openaicompat"
	"github.com/livingdolls/orkoda-tui/internal/llmprovider"
	"github.com/livingdolls/orkoda-tui/internal/observability"
	"github.com/livingdolls/orkoda-tui/internal/planningagent"
	"github.com/livingdolls/orkoda-tui/internal/planningcontext"
	"github.com/livingdolls/orkoda-tui/internal/plans"
	"github.com/livingdolls/orkoda-tui/internal/projects"
	"github.com/livingdolls/orkoda-tui/internal/publication"
	"github.com/livingdolls/orkoda-tui/internal/repositorysummary"
	"github.com/livingdolls/orkoda-tui/internal/reviewer"
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
	cfg.APIToken, err = config.EnsureAPIToken(cfg.APITokenFile, cfg.APIToken)
	if err != nil {
		return err
	}
	instanceLock, err := instance.Acquire(filepath.Join(cfg.DataDir, "daemon.lock"))
	if err != nil {
		return err
	}
	defer instanceLock.Release()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	metrics := observability.New()

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	runtimeCtx, stopRuntime := context.WithCancel(signalCtx)
	defer stopRuntime()

	if err := database.Backup(runtimeCtx, cfg.DatabasePath); err != nil {
		return err
	}
	db, err := database.Open(runtimeCtx, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := database.Migrate(runtimeCtx, db); err != nil {
		return err
	}
	if err := database.CheckIntegrity(runtimeCtx, db); err != nil {
		return err
	}
	artifactStore, err := artifact.NewLocalStore(cfg.ArtifactDir)
	if err != nil {
		return err
	}
	diagnosticsService, err := diagnostics.NewService(db, artifactStore)
	if err != nil {
		return err
	}
	logger.Info("sqlite ready", "path", cfg.DatabasePath)

	activityRepository := activity.NewRepository(db)
	idempotencyStore, err := httpapi.NewSQLIdempotencyStore(db)
	if err != nil {
		return err
	}
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

	credentialStore, err := credentials.NewAutoStore("orkoda", filepath.Join(cfg.DataDir, "credentials.json"))
	if err != nil {
		return err
	}
	localPlanningProvider := planningagent.NewLocalFakeProvider()
	providerRegistry, err := llm.NewRegistry(localPlanningProvider)
	if err != nil {
		return err
	}
	bootstrapProviders := make([]llmprovider.Bootstrap, 0, len(cfg.LLM.Providers))
	for _, configured := range cfg.LLM.Providers {
		provider, err := openaicompat.New(openaicompat.Config{
			Name: configured.Name, BaseURL: configured.BaseURL, APIKey: configured.APIKey,
			DefaultModel: configured.Model, Headers: configured.Headers,
			Timeout: configured.Timeout, JSONMode: openaicompat.JSONMode(configured.JSONMode),
		})
		if err != nil {
			return fmt.Errorf("configure LLM provider %s: %w", configured.Name, err)
		}
		bootstrapProviders = append(bootstrapProviders, llmprovider.Bootstrap{
			Provider: provider, BaseURL: configured.BaseURL, DefaultModel: configured.Model,
			JSONMode: configured.JSONMode, Timeout: configured.Timeout,
		})
	}
	defaultProvider := cfg.LLM.Provider
	defaultModel := cfg.LLM.Model
	if defaultProvider == "" || defaultProvider == planningagent.LocalFakeProviderName {
		defaultProvider = planningagent.LocalFakeProviderName
		defaultModel = planningagent.LocalFakeModelName
	}
	providerConfigRepository, err := llmprovider.NewRepository(db)
	if err != nil {
		return err
	}
	providerService, err := llmprovider.NewService(
		providerConfigRepository,
		providerRegistry,
		credentialStore,
		defaultProvider,
		bootstrapProviders,
	)
	if err != nil {
		return err
	}
	if err := providerService.Load(runtimeCtx); err != nil {
		return err
	}
	providerCatalog := providerService

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
	interruptedPlanningRuns, err := planningAgentService.RecoverInterruptedRuns(runtimeCtx)
	if err != nil {
		return err
	}
	if interruptedPlanningRuns > 0 {
		logger.Warn("recovered interrupted planning runs", "count", interruptedPlanningRuns)
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
	if report, err := workspaceRepository.ReconcileOrphans(runtimeCtx); err != nil {
		return err
	} else if len(report.Orphaned) > 0 || len(report.Missing) > 0 {
		logger.Warn("workspace filesystem reconciled", "orphaned", len(report.Orphaned), "missing", len(report.Missing))
	}
	executionRepository, err := execution.NewRepository(db)
	if err != nil {
		return err
	}
	recoveredExecutorFailures, err := executionRepository.ReconcileFailedWorkflows(
		runtimeCtx,
		workflowJobRepository,
	)
	if err != nil {
		return err
	}
	if recoveredExecutorFailures > 0 {
		logger.Warn(
			"recovered workflows with failed Executor executions",
			"count",
			recoveredExecutorFailures,
		)
	}
	recoveredDeadExecutorDispatches, err := executionRepository.ReconcileDeadExecutionDispatches(
		runtimeCtx,
		workflowJobRepository,
	)
	if err != nil {
		return err
	}
	if recoveredDeadExecutorDispatches > 0 {
		logger.Warn(
			"recovered workflows with dead Executor dispatches",
			"count",
			recoveredDeadExecutorDispatches,
		)
	}
	checkRepository, err := checks.NewRepository(db)
	if err != nil {
		return err
	}
	reviewRepository, err := reviewer.NewRepository(db)
	if err != nil {
		return err
	}
	approvalRepository, err := approval.NewRepository(db)
	if err != nil {
		return err
	}
	approvalService, err := approval.NewService(
		approvalRepository,
		workflowJobRepository,
		executionRepository,
		reviewRepository,
		checkRepository,
		activityRecorder,
	)
	if err != nil {
		return err
	}
	reviewContextBuilder, err := reviewer.NewContextBuilder(db)
	if err != nil {
		return err
	}
	contextSelector, err := execution.NewContextSelector(db)
	if err != nil {
		return err
	}
	executorRunner, err := execution.NewLLMRunner(llmGateway, contextSelector, executionRepository)
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
		executorRunner,
		activityRecorder,
		workerID,
		cfg.WorkspaceLeaseTTL,
		defaultProvider,
		defaultModel,
		artifactStore,
	)
	if err != nil {
		return err
	}
	var checkRunner checks.Runner
	if cfg.SandboxMode == "host" {
		checkRunner = checks.CommandRunner{}
	} else {
		checkRunner = checks.NewDockerRunner(cfg.SandboxImage)
	}
	checkHandler, err := checks.NewHandler(
		workflowJobRepository,
		executionRepository,
		workspaceRepository,
		checkRepository,
		checks.NewDetector(),
		checkRunner,
		activityRecorder,
		workerID,
		cfg.WorkspaceLeaseTTL,
		artifactStore,
	)
	if err != nil {
		return err
	}
	reviewHandler, err := reviewer.NewHandler(
		workflowJobRepository,
		executionRepository,
		checkRepository,
		agentSettingsRepository,
		reviewRepository,
		reviewContextBuilder,
		llmGateway,
		activityRecorder,
		defaultProvider,
		defaultModel,
	)
	if err != nil {
		return err
	}
	publicationRepository, err := publication.NewRepository(db)
	if err != nil {
		return err
	}
	githubPublisher, err := publication.NewGitHubPublisher(credentialStore)
	if err != nil {
		return err
	}
	publicationHandler, err := publication.NewHandler(
		workflowJobRepository,
		workspaceRepository,
		approvalRepository,
		checkRepository,
		publicationRepository,
		activityRecorder,
		workerID,
		cfg.WorkspaceLeaseTTL,
	)
	if err != nil {
		return err
	}
	logger.Info(
		"workspace runtime ready",
		"root", workspaceRepository.Root(),
		"lease_ttl", cfg.WorkspaceLeaseTTL,
	)

	schedulerConfig := scheduler.DefaultConfig(workerID)
	schedulerConfig.Metrics = metrics
	queueScheduler, err := scheduler.New(
		queue,
		schedulerConfig,
		map[string]scheduler.Handler{
			"system.noop": func(_ context.Context, job jobqueue.Job) error {
				logger.Info("noop job handled", "job_id", job.ID)
				return nil
			},
			"workflow.prepare_workspace": prepareWorkspaceHandler.HandleDurable,
			"workflow.execute":           executeHandler.HandleDurable,
			"workflow.run_checks":        checkHandler.HandleDurable,
			"workflow.review":            reviewHandler.HandleDurable,
			"workflow.publish":           publicationHandler.HandleDurable,
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
				Repositories:        projectRepository,
				RepositorySummaries: summaryRepository,
				PlanningContexts:    planningContextRepository,
				PlanningAgent:       planningAgentService,
				AgentSettings:       agentSettingsRepository,
				WorkflowJobs:        workflowJobRepository,
				Workspaces:          workspaceRepository,
				Executions:          executionRepository,
				Checks:              checkRepository,
				Reviews:             reviewRepository,
				Approvals:           approvalService,
				LLMProviders:        providerCatalog,
				LLMProviderAdmin:    providerService,
				LLMPolicy:           llmGateway,
				DefaultLLMProvider:  defaultProvider,
				DefaultLLMModel:     defaultModel,
				APIToken:            cfg.APIToken,
				LiveEvents:          liveEvents,
				Idempotency:         idempotencyStore,
				Publications:        publicationRepository,
				RemotePublisher:     githubPublisher,
				Diagnostics:         diagnosticsService,
				Metrics:             metrics,
				Artifacts:           artifactStore,
			},
		),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 * 1024,
	}
	unixListener, err := openUnixListener(cfg.APISocketPath())
	if err != nil {
		return err
	}
	defer os.Remove(cfg.APISocketPath())

	results := make(chan componentResult, 3)
	go func() {
		logger.Info("daemon listening", "address", cfg.APIAddress(), "environment", cfg.Environment)
		results <- componentResult{name: "http", err: server.ListenAndServe()}
	}()
	go func() {
		logger.Info("daemon listening", "unix_socket", cfg.APISocketPath())
		results <- componentResult{name: "unix", err: server.Serve(unixListener)}
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

	for completed < 3 {
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
	if (result.name == "http" || result.name == "unix") && errors.Is(result.err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("%s component stopped: %w", result.name, result.err)
}

func openUnixListener(path string) (net.Listener, error) {
	path = filepath.Clean(path)
	if path == "." || path == string(filepath.Separator) {
		return nil, fmt.Errorf("Unix socket path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create Unix socket directory: %w", err)
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("refusing to replace non-socket Unix path %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove stale Unix socket: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect Unix socket: %w", err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on Unix socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("restrict Unix socket permissions: %w", err)
	}
	return listener, nil
}
