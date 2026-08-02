package agentconfig

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/livingdolls/orkoda-tui/internal/database"
)

type recordedEvent struct {
	eventType string
	payload   any
}

type fakeRecorder struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (f *fakeRecorder) Record(_ context.Context, _ string, eventType string, payload any, _ time.Time) error {
	f.mu.Lock()
	f.events = append(f.events, recordedEvent{eventType: eventType, payload: payload})
	f.mu.Unlock()
	return nil
}

func openAgentRepository(t *testing.T) (*Repository, *sql.DB, *fakeRecorder, string) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "orkoda.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		db.Close()
		t.Fatalf("database.Migrate() error = %v", err)
	}
	projectID := "project-1"
	now := time.Now().UTC().UnixMilli()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)
	`, projectID, "Example", now, now); err != nil {
		db.Close()
		t.Fatalf("insert project: %v", err)
	}
	recorder := &fakeRecorder{}
	repository, err := NewRepository(db, recorder)
	if err != nil {
		db.Close()
		t.Fatalf("NewRepository() error = %v", err)
	}
	return repository, db, recorder, projectID
}

func TestGetSeedsDenyByDefaultSettings(t *testing.T) {
	repository, db, _, projectID := openAgentRepository(t)
	defer db.Close()

	settings, err := repository.Get(context.Background(), projectID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if settings.ProjectID != projectID || settings.Version != 1 {
		t.Fatalf("settings = %#v", settings)
	}
	if len(settings.Agents) != 3 || len(settings.ToolPolicies) != 3 {
		t.Fatalf("settings roles = %#v", settings)
	}
	if settings.Agents[0].Role != RolePlanner || settings.Agents[1].Role != RoleExecutor || settings.Agents[2].Role != RoleReviewer {
		t.Fatalf("agent order = %#v", settings.Agents)
	}
	executor := settings.ToolPolicies[1]
	if executor.NetworkAccess != NetworkDisabled || executor.FilesystemAccess != FilesystemWorkspaceWrite {
		t.Fatalf("executor policy = %#v", executor)
	}
	if contains(executor.AllowedTools, ToolCommandRun) || len(executor.AllowedCommandProfiles) != 0 {
		t.Fatalf("default executor command policy is not deny-by-default: %#v", executor)
	}
}

func TestUpdatePersistsAggregateAndRecordsEvent(t *testing.T) {
	repository, db, recorder, projectID := openAgentRepository(t)
	defer db.Close()
	ctx := context.Background()

	settings, err := repository.Get(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	settings.Agents[1].Provider = " OpenRouter "
	settings.Agents[1].Model = "example/executor"
	settings.Agents[1].SystemInstruction = " Make the smallest safe change. "
	settings.ToolPolicies[1].AllowedTools = append(settings.ToolPolicies[1].AllowedTools, ToolCommandRun)
	settings.ToolPolicies[1].AllowedCommandProfiles = []string{" test.go ", "test.go"}
	settings.ToolPolicies[1].NetworkAccess = NetworkLoopback

	updated, err := repository.Update(ctx, projectID, UpdateInput{
		ExpectedVersion: settings.Version,
		Agents:          settings.Agents,
		ToolPolicies:    settings.ToolPolicies,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Version != 2 || updated.Agents[1].Provider != "openrouter" {
		t.Fatalf("updated settings = %#v", updated)
	}
	if updated.Agents[1].SystemInstruction != "Make the smallest safe change." {
		t.Fatalf("system instruction = %q", updated.Agents[1].SystemInstruction)
	}
	if profiles := updated.ToolPolicies[1].AllowedCommandProfiles; len(profiles) != 1 || profiles[0] != "test.go" {
		t.Fatalf("command profiles = %#v", profiles)
	}

	reloaded, err := repository.Get(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Version != 2 || reloaded.ToolPolicies[1].NetworkAccess != NetworkLoopback {
		t.Fatalf("reloaded settings = %#v", reloaded)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.events) != 1 || recorder.events[0].eventType != "agent.settings_updated" {
		t.Fatalf("events = %#v", recorder.events)
	}
}

func TestUpdateRejectsStaleVersionWithoutPartialWrite(t *testing.T) {
	repository, db, _, projectID := openAgentRepository(t)
	defer db.Close()
	ctx := context.Background()
	settings, err := repository.Get(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	settings.Agents[0].Enabled = false

	_, err = repository.Update(ctx, projectID, UpdateInput{
		ExpectedVersion: settings.Version + 1,
		Agents:          settings.Agents,
		ToolPolicies:    settings.ToolPolicies,
	})
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("Update() error = %v", err)
	}
	reloaded, err := repository.Get(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Version != 1 || !reloaded.Agents[0].Enabled {
		t.Fatalf("stale update mutated settings: %#v", reloaded)
	}
}

func TestValidationRejectsUnsafePolicies(t *testing.T) {
	repository, db, _, projectID := openAgentRepository(t)
	defer db.Close()
	ctx := context.Background()
	base, err := repository.Get(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(*Settings)
	}{
		{
			name: "unknown tool",
			mutate: func(settings *Settings) {
				settings.ToolPolicies[1].AllowedTools = append(settings.ToolPolicies[1].AllowedTools, "shell")
			},
		},
		{
			name: "reviewer write",
			mutate: func(settings *Settings) {
				settings.ToolPolicies[2].FilesystemAccess = FilesystemWorkspaceWrite
			},
		},
		{
			name: "command without profile",
			mutate: func(settings *Settings) {
				settings.ToolPolicies[1].AllowedTools = append(settings.ToolPolicies[1].AllowedTools, ToolCommandRun)
			},
		},
		{
			name: "profile without command",
			mutate: func(settings *Settings) {
				settings.ToolPolicies[1].AllowedCommandProfiles = []string{"test.go"}
			},
		},
		{
			name: "provider without model",
			mutate: func(settings *Settings) {
				settings.Agents[0].Provider = "openrouter"
			},
		},
		{
			name: "planner network",
			mutate: func(settings *Settings) {
				settings.ToolPolicies[0].NetworkAccess = NetworkLoopback
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := cloneSettings(base)
			test.mutate(&settings)
			_, err := repository.Update(ctx, projectID, UpdateInput{
				ExpectedVersion: settings.Version,
				Agents:          settings.Agents,
				ToolPolicies:    settings.ToolPolicies,
			})
			if !errors.Is(err, ErrInvalidSettings) {
				t.Fatalf("Update() error = %v", err)
			}
		})
	}
}

func TestMissingProjectAndCascadeDelete(t *testing.T) {
	repository, db, _, projectID := openAgentRepository(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := repository.Get(ctx, "missing"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("missing project error = %v", err)
	}
	if _, err := repository.Get(ctx, projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, projectID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	for _, table := range []string{"agent_settings", "agent_configs", "tool_policies"} {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE project_id = ?`, projectID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d", table, count)
		}
	}
}

func TestConcurrentGetSeedsOneAggregate(t *testing.T) {
	repository, db, _, projectID := openAgentRepository(t)
	defer db.Close()
	ctx := context.Background()

	var wait sync.WaitGroup
	errorsChannel := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := repository.Get(ctx, projectID)
			errorsChannel <- err
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("concurrent Get() error = %v", err)
		}
	}
	var aggregateCount, agentCount, policyCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_settings WHERE project_id = ?`, projectID).Scan(&aggregateCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_configs WHERE project_id = ?`, projectID).Scan(&agentCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tool_policies WHERE project_id = ?`, projectID).Scan(&policyCount); err != nil {
		t.Fatal(err)
	}
	if aggregateCount != 1 || agentCount != 3 || policyCount != 3 {
		t.Fatalf("aggregate=%d agents=%d policies=%d", aggregateCount, agentCount, policyCount)
	}
}

func cloneSettings(settings Settings) Settings {
	cloned := settings
	cloned.Agents = append([]AgentConfig(nil), settings.Agents...)
	cloned.ToolPolicies = append([]ToolPolicy(nil), settings.ToolPolicies...)
	for index := range cloned.ToolPolicies {
		cloned.ToolPolicies[index].AllowedTools = append([]string(nil), settings.ToolPolicies[index].AllowedTools...)
		cloned.ToolPolicies[index].AllowedCommandProfiles = append([]string(nil), settings.ToolPolicies[index].AllowedCommandProfiles...)
	}
	return cloned
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
