from pathlib import Path


def replace_once(source: str, old: str, new: str, label: str) -> str:
    count = source.count(old)
    if count != 1:
        raise SystemExit(f"expected one {label}, found {count}")
    return source.replace(old, new, 1)


# Let synchronous planning requests live longer than the daemon's default
# two-minute wall-clock budget.
client_path = Path("apps/tui/src/planning-agent.ts")
client = client_path.read_text()
client = replace_once(
    client,
    'type ErrorResponse = { error?: { message?: string } }\n',
    'type ErrorResponse = { error?: { message?: string } }\n\nconst planningAgentRequestTimeoutMs = 5 * 60 * 1000\n',
    "planning client timeout constant",
)
client = replace_once(
    client,
    '  const timeout = setTimeout(() => controller.abort(), 30000)',
    '  const timeout = setTimeout(() => controller.abort(), planningAgentRequestTimeoutMs)',
    "planning client timeout",
)
client_path.write_text(client)


# Poll while persisted cards remain in PLANNING and describe RUNNING as an
# in-progress state rather than a finished state.
board_path = Path("apps/tui/src/board-screen.tsx")
board = board_path.read_text()
board = replace_once(
    board,
    '''  const selectedItem = columnItems[selectedIndex] ?? null
  const selectedActions = selectedItem ? boardActions(selectedItem) : []
''',
    '''  const selectedItem = columnItems[selectedIndex] ?? null
  const selectedActions = selectedItem ? boardActions(selectedItem) : []
  const hasPlanningItems = items.some(
    (item) => !item.workflow && item.plan.status === "PLANNING",
  )
''',
    "planning item detector",
)
board = replace_once(
    board,
    '''  useEffect(() => {
    if (!lastEvent || mode !== "board" || actionMenuOpen) return
    const timeout = setTimeout(() => void reload(), 120)
    return () => clearTimeout(timeout)
  }, [lastEvent, mode, actionMenuOpen, reload])

  useEffect(() => {
    onInteractionChange?.(mode !== "board" || actionMenuOpen || busy)
''',
    '''  useEffect(() => {
    if (!lastEvent || mode !== "board" || actionMenuOpen) return
    const timeout = setTimeout(() => void reload(), 120)
    return () => clearTimeout(timeout)
  }, [lastEvent, mode, actionMenuOpen, reload])

  useEffect(() => {
    if (
      connection.state !== "connected" ||
      mode !== "board" ||
      actionMenuOpen ||
      busy ||
      loadState === "loading" ||
      !hasPlanningItems
    ) {
      return
    }
    const timeout = setTimeout(() => void reload(), 1500)
    return () => clearTimeout(timeout)
  }, [
    connection.state,
    mode,
    actionMenuOpen,
    busy,
    loadState,
    hasPlanningItems,
    reload,
  ])

  useEffect(() => {
    onInteractionChange?.(mode !== "board" || actionMenuOpen || busy)
''',
    "planning polling effect",
)
board = replace_once(
    board,
    '''        setMessage(
          run.status === "COMPLETED"
            ? `Plan prepared with ${run.result?.steps.length ?? 0} implementation step(s).`
            : `Planning finished with status ${run.status}.`,
        )
''',
    '''        setMessage(
          run.status === "COMPLETED"
            ? `Plan prepared with ${run.result?.steps.length ?? 0} implementation step(s).`
            : run.status === "RUNNING"
              ? "Planning Agent is still running. The card will refresh automatically."
              : run.error_message || `Planning ended with status ${run.status}.`,
        )
''',
    "planning run message",
)
board_path.write_text(board)


# Persist terminal status with a detached cleanup context and recover runs that
# could not have survived a daemon restart.
service_path = Path("internal/planningagent/service.go")
service = service_path.read_text()
old_fail = '''func (s *Service) failRun(ctx context.Context, run Run, cause error) {
\tstatus := RunStatusFailed
\tcode := llm.ErrorUnknown
\tmessage := "planning agent failed"
\tif providerError, ok := llm.AsProviderError(cause); ok {
\t\tcode = providerError.Code
\t\tmessage = providerError.Message
\t\tif providerError.Code == llm.ErrorCancelled {
\t\t\tstatus = RunStatusCancelled
\t\t}
\t}
\tnow := time.Now().UTC()
\t_, err := s.db.ExecContext(ctx, `
\t\tUPDATE planning_runs
\t\tSET status = ?, error_code = ?, error_message = ?, updated_at = ?
\t\tWHERE id = ? AND status = ?
\t`, status, code, message, now.UnixMilli(), run.ID, RunStatusRunning)
\tif err != nil {
\t\tslog.Warn("store failed planning run", "run_id", run.ID, "error", err)
\t}
\tif plan, err := s.plans.Get(ctx, run.PlanID); err == nil {
\t\tif _, err := s.plans.Update(ctx, plan.ID, plan.Title, plans.StatusDraft); err != nil {
\t\t\tslog.Warn("reset failed plan status", "plan_id", run.PlanID, "error", err)
\t\t}
\t}
\ts.record(ctx, "planning.agent_failed", map[string]any{
\t\t"run_id": run.ID, "plan_id": run.PlanID, "status": status, "error_code": code,
\t}, now)
}
'''
new_fail = '''func (s *Service) RecoverInterruptedRuns(ctx context.Context) (int, error) {
\trows, err := s.db.QueryContext(ctx, `
\t\tSELECT id, plan_id FROM planning_runs WHERE status = ?
\t`, RunStatusRunning)
\tif err != nil {
\t\treturn 0, fmt.Errorf("list interrupted planning runs: %w", err)
\t}
\tinterrupted := make([]Run, 0)
\tfor rows.Next() {
\t\tvar run Run
\t\tif err := rows.Scan(&run.ID, &run.PlanID); err != nil {
\t\t\trows.Close()
\t\t\treturn 0, fmt.Errorf("scan interrupted planning run: %w", err)
\t\t}
\t\tinterrupted = append(interrupted, run)
\t}
\tif err := rows.Err(); err != nil {
\t\trows.Close()
\t\treturn 0, fmt.Errorf("iterate interrupted planning runs: %w", err)
\t}
\tif err := rows.Close(); err != nil {
\t\treturn 0, fmt.Errorf("close interrupted planning runs: %w", err)
\t}
\tif len(interrupted) == 0 {
\t\treturn 0, nil
\t}

\tnow := time.Now().UTC()
\tmessage := "planning was interrupted before completion; start it again"
\ttx, err := s.db.BeginTx(ctx, nil)
\tif err != nil {
\t\treturn 0, fmt.Errorf("begin interrupted planning recovery: %w", err)
\t}
\tdefer tx.Rollback()
\tfor _, run := range interrupted {
\t\tresult, err := tx.ExecContext(ctx, `
\t\t\tUPDATE planning_runs
\t\t\tSET status = ?, error_code = ?, error_message = ?, updated_at = ?
\t\t\tWHERE id = ? AND status = ?
\t\t`, RunStatusCancelled, llm.ErrorCancelled, message, now.UnixMilli(), run.ID, RunStatusRunning)
\t\tif err != nil {
\t\t\treturn 0, fmt.Errorf("recover interrupted planning run: %w", err)
\t\t}
\t\tif err := requireOne(result, ErrRunNotFound); err != nil {
\t\t\treturn 0, err
\t\t}
\t\tif _, err := tx.ExecContext(ctx, `
\t\t\tUPDATE plans SET status = ?, updated_at = ?
\t\t\tWHERE id = ? AND status = ?
\t\t`, plans.StatusDraft, now.UnixMilli(), run.PlanID, plans.StatusPlanning); err != nil {
\t\t\treturn 0, fmt.Errorf("reset interrupted plan: %w", err)
\t\t}
\t}
\tif err := tx.Commit(); err != nil {
\t\treturn 0, fmt.Errorf("commit interrupted planning recovery: %w", err)
\t}
\tfor _, run := range interrupted {
\t\ts.record(ctx, "planning.agent_interrupted", map[string]any{
\t\t\t"run_id": run.ID, "plan_id": run.PlanID, "status": RunStatusCancelled,
\t\t}, now)
\t}
\treturn len(interrupted), nil
}

func (s *Service) failRun(ctx context.Context, run Run, cause error) {
\tstatus := RunStatusFailed
\tcode := llm.ErrorUnknown
\tmessage := "planning agent failed"
\tif providerError, ok := llm.AsProviderError(cause); ok {
\t\tcode = providerError.Code
\t\tmessage = providerError.Message
\t\tif providerError.Code == llm.ErrorCancelled {
\t\t\tstatus = RunStatusCancelled
\t\t}
\t}
\tcleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
\tdefer cancel()
\tnow := time.Now().UTC()
\t_, err := s.db.ExecContext(cleanupCtx, `
\t\tUPDATE planning_runs
\t\tSET status = ?, error_code = ?, error_message = ?, updated_at = ?
\t\tWHERE id = ? AND status = ?
\t`, status, code, message, now.UnixMilli(), run.ID, RunStatusRunning)
\tif err != nil {
\t\tslog.Warn("store failed planning run", "run_id", run.ID, "error", err)
\t}
\tif plan, err := s.plans.Get(cleanupCtx, run.PlanID); err == nil {
\t\tif _, err := s.plans.Update(cleanupCtx, plan.ID, plan.Title, plans.StatusDraft); err != nil {
\t\t\tslog.Warn("reset failed plan status", "plan_id", run.PlanID, "error", err)
\t\t}
\t}
\ts.record(cleanupCtx, "planning.agent_failed", map[string]any{
\t\t"run_id": run.ID, "plan_id": run.PlanID, "status": status, "error_code": code,
\t}, now)
}
'''
service = replace_once(service, old_fail, new_fail, "planning failure persistence")
service_path.write_text(service)


main_path = Path("cmd/api/main.go")
main = main_path.read_text()
main = replace_once(
    main,
    '''\tplanningAgentService, err := planningagent.NewService(
\t\tdb,
\t\tplanningContextRepository,
\t\tplanRepository,
\t\tllmGateway,
\t\tactivityRecorder,
\t)
\tif err != nil {
\t\treturn err
\t}

\tqueue := jobqueue.New(db)
''',
    '''\tplanningAgentService, err := planningagent.NewService(
\t\tdb,
\t\tplanningContextRepository,
\t\tplanRepository,
\t\tllmGateway,
\t\tactivityRecorder,
\t)
\tif err != nil {
\t\treturn err
\t}
\tinterruptedPlanningRuns, err := planningAgentService.RecoverInterruptedRuns(runtimeCtx)
\tif err != nil {
\t\treturn err
\t}
\tif interruptedPlanningRuns > 0 {
\t\tlogger.Warn("recovered interrupted planning runs", "count", interruptedPlanningRuns)
\t}

\tqueue := jobqueue.New(db)
''',
    "planning startup recovery",
)
main_path.write_text(main)


# Regression coverage: cancellation must not leave RUNNING behind, and startup
# recovery must make an interrupted plan retryable.
test_path = Path("internal/planningagent/service_test.go")
test = test_path.read_text()
test = replace_once(
    test,
    '''type mutableContextReader struct {
\tvalue planningcontext.Context
\terr   error
}
''',
    '''type mutableContextReader struct {
\tvalue planningcontext.Context
\terr   error
}

type cancellingGateway struct {
\tcancel context.CancelFunc
}

func (g cancellingGateway) Complete(context.Context, string, llm.Request) (llm.Response, error) {
\tg.cancel()
\treturn llm.Response{}, context.Canceled
}
''',
    "cancelling planning gateway",
)
new_tests = r'''func TestPlanningAgentPersistsFailureAfterRequestCancellation(t *testing.T) {
	background := context.Background()
	db, err := database.Open(background, filepath.Join(t.TempDir(), "cancelled.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(background, db); err != nil {
		t.Fatal(err)
	}

	planRepository, planningContext := seedPlanningAgentState(t, background, db)
	runCtx, cancel := context.WithCancel(background)
	service, err := NewService(
		db,
		&mutableContextReader{value: planningContext},
		planRepository,
		cancellingGateway{cancel: cancel},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Start(runCtx, planningContext.PlanID, "fake", "fake-model"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancelled planning request, got %v", err)
	}
	current, err := service.Current(background, planningContext.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status == RunStatusRunning {
		t.Fatalf("cancelled request left planning run active: %#v", current)
	}
	plan, err := planRepository.Get(background, planningContext.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != plans.StatusDraft {
		t.Fatalf("expected retryable DRAFT plan, got %s", plan.Status)
	}
}

func TestRecoverInterruptedPlanningRuns(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	planRepository, planningContext := seedPlanningAgentState(t, ctx, db)
	service, err := NewService(
		db,
		&mutableContextReader{value: planningContext},
		planRepository,
		cancellingGateway{cancel: func() {}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.createRun(ctx, planningContext, "", "fake", "fake-model")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planRepository.Get(ctx, planningContext.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planRepository.Update(ctx, plan.ID, plan.Title, plans.StatusPlanning); err != nil {
		t.Fatal(err)
	}

	recovered, err := service.RecoverInterruptedRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("expected one recovered run, got %d", recovered)
	}
	storedRun, err := service.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedRun.Status != RunStatusCancelled || storedRun.ErrorCode != llm.ErrorCancelled {
		t.Fatalf("expected cancelled interrupted run, got %#v", storedRun)
	}
	storedPlan, err := planRepository.Get(ctx, planningContext.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if storedPlan.Status != plans.StatusDraft {
		t.Fatalf("expected recovered DRAFT plan, got %s", storedPlan.Status)
	}
	secondRecovery, err := service.RecoverInterruptedRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if secondRecovery != 0 {
		t.Fatalf("expected idempotent recovery, got %d", secondRecovery)
	}
}

'''
test = replace_once(
    test,
    'func seedPlanningAgentState(\n',
    new_tests + 'func seedPlanningAgentState(\n',
    "planning recovery tests",
)
test_path.write_text(test)
