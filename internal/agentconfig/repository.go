package agentconfig

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

var (
	ErrProjectNotFound = errors.New("project not found")
	ErrInvalidSettings = errors.New("invalid agent settings")
	ErrVersionConflict = errors.New("agent settings version conflict")
)

type Role string

const (
	RolePlanner  Role = "PLANNER"
	RoleExecutor Role = "EXECUTOR"
	RoleReviewer Role = "REVIEWER"
)

type NetworkAccess string

const (
	NetworkDisabled NetworkAccess = "DISABLED"
	NetworkLoopback NetworkAccess = "LOOPBACK"
	NetworkOutbound NetworkAccess = "OUTBOUND"
)

type FilesystemAccess string

const (
	FilesystemReadOnly       FilesystemAccess = "READ_ONLY"
	FilesystemWorkspaceWrite FilesystemAccess = "WORKSPACE_WRITE"
)

const (
	ToolFileRead   = "file_read"
	ToolFileSearch = "file_search"
	ToolFilePatch  = "file_patch"
	ToolFileCreate = "file_create"
	ToolFileDelete = "file_delete"
	ToolGitStatus  = "git_status"
	ToolGitDiff    = "git_diff"
	ToolCommandRun = "command_run"
	maxInstruction = 8000
)

var (
	roles      = []Role{RolePlanner, RoleExecutor, RoleReviewer}
	knownTools = map[string]struct{}{
		ToolFileRead: {}, ToolFileSearch: {}, ToolFilePatch: {}, ToolFileCreate: {},
		ToolFileDelete: {}, ToolGitStatus: {}, ToolGitDiff: {}, ToolCommandRun: {},
	}
	readOnlyTools = map[string]struct{}{
		ToolFileRead: {}, ToolFileSearch: {}, ToolGitStatus: {}, ToolGitDiff: {},
	}
	commandProfilePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

type EventRecorder interface {
	Record(context.Context, string, string, any, time.Time) error
}

type AgentConfig struct {
	Role              Role    `json:"role"`
	Provider          string  `json:"provider"`
	Model             string  `json:"model"`
	Temperature       float64 `json:"temperature"`
	MaxOutputTokens   int     `json:"max_output_tokens"`
	Enabled           bool    `json:"enabled"`
	SystemInstruction string  `json:"system_instruction"`
}

type ToolPolicy struct {
	Role                   Role             `json:"role"`
	AllowedTools           []string         `json:"allowed_tools"`
	AllowedCommandProfiles []string         `json:"allowed_command_profiles"`
	NetworkAccess          NetworkAccess    `json:"network_access"`
	FilesystemAccess       FilesystemAccess `json:"filesystem_access"`
	CommandTimeoutMS       int              `json:"command_timeout_ms"`
	MaxCommandOutputBytes  int              `json:"max_command_output_bytes"`
	MaxFileBytes           int              `json:"max_file_bytes"`
	MaxPatchBytes          int              `json:"max_patch_bytes"`
}

type Settings struct {
	ProjectID    string        `json:"project_id"`
	Version      int           `json:"version"`
	Agents       []AgentConfig `json:"agents"`
	ToolPolicies []ToolPolicy  `json:"tool_policies"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

type UpdateInput struct {
	ExpectedVersion int           `json:"expected_version"`
	Agents          []AgentConfig `json:"agents"`
	ToolPolicies    []ToolPolicy  `json:"tool_policies"`
}

type Repository struct {
	db       *sql.DB
	recorder EventRecorder
}

func NewRepository(db *sql.DB, recorder EventRecorder) (*Repository, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	return &Repository{db: db, recorder: recorder}, nil
}

func (r *Repository) Get(ctx context.Context, projectID string) (Settings, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return Settings{}, fmt.Errorf("%w: project ID is required", ErrInvalidSettings)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Settings{}, fmt.Errorf("begin agent settings read: %w", err)
	}
	defer tx.Rollback()

	if err := ensureDefaults(ctx, tx, projectID); err != nil {
		return Settings{}, err
	}
	settings, err := loadSettings(ctx, tx, projectID)
	if err != nil {
		return Settings{}, err
	}
	if err := tx.Commit(); err != nil {
		return Settings{}, fmt.Errorf("commit agent settings defaults: %w", err)
	}
	return settings, nil
}

func (r *Repository) Update(ctx context.Context, projectID string, input UpdateInput) (Settings, error) {
	projectID = strings.TrimSpace(projectID)
	input.Agents = normalizeAgents(input.Agents)
	input.ToolPolicies = normalizePolicies(input.ToolPolicies)
	if projectID == "" {
		return Settings{}, fmt.Errorf("%w: project ID is required", ErrInvalidSettings)
	}
	if input.ExpectedVersion < 1 {
		return Settings{}, fmt.Errorf("%w: expected_version must be positive", ErrInvalidSettings)
	}
	if err := validateAggregate(input.Agents, input.ToolPolicies); err != nil {
		return Settings{}, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Settings{}, fmt.Errorf("begin agent settings update: %w", err)
	}
	defer tx.Rollback()
	if err := ensureDefaults(ctx, tx, projectID); err != nil {
		return Settings{}, err
	}

	var currentVersion int
	if err := tx.QueryRowContext(ctx, `SELECT version FROM agent_settings WHERE project_id = ?`, projectID).Scan(&currentVersion); err != nil {
		return Settings{}, fmt.Errorf("read current agent settings version: %w", err)
	}
	if currentVersion != input.ExpectedVersion {
		return Settings{}, fmt.Errorf("%w: expected %d, current %d", ErrVersionConflict, input.ExpectedVersion, currentVersion)
	}

	now := time.Now().UTC()
	nextVersion := currentVersion + 1
	result, err := tx.ExecContext(ctx, `
		UPDATE agent_settings SET version = ?, updated_at = ?
		WHERE project_id = ? AND version = ?
	`, nextVersion, now.UnixMilli(), projectID, currentVersion)
	if err != nil {
		return Settings{}, fmt.Errorf("update agent settings version: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return Settings{}, fmt.Errorf("read updated agent settings rows: %w", err)
	} else if affected != 1 {
		return Settings{}, ErrVersionConflict
	}

	for _, config := range input.Agents {
		if _, err := tx.ExecContext(ctx, `
			UPDATE agent_configs
			SET provider = ?, model = ?, temperature = ?, max_output_tokens = ?,
				enabled = ?, system_instruction = ?, updated_at = ?
			WHERE project_id = ? AND role = ?
		`, config.Provider, config.Model, config.Temperature, config.MaxOutputTokens,
			boolInteger(config.Enabled), config.SystemInstruction, now.UnixMilli(), projectID, config.Role); err != nil {
			return Settings{}, fmt.Errorf("update %s agent config: %w", config.Role, err)
		}
	}
	for _, policy := range input.ToolPolicies {
		toolsJSON, err := json.Marshal(policy.AllowedTools)
		if err != nil {
			return Settings{}, fmt.Errorf("marshal %s allowed tools: %w", policy.Role, err)
		}
		profilesJSON, err := json.Marshal(policy.AllowedCommandProfiles)
		if err != nil {
			return Settings{}, fmt.Errorf("marshal %s command profiles: %w", policy.Role, err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE tool_policies
			SET allowed_tools_json = ?, allowed_command_profiles_json = ?,
				network_access = ?, filesystem_access = ?, command_timeout_ms = ?,
				max_command_output_bytes = ?, max_file_bytes = ?, max_patch_bytes = ?, updated_at = ?
			WHERE project_id = ? AND role = ?
		`, string(toolsJSON), string(profilesJSON), policy.NetworkAccess, policy.FilesystemAccess,
			policy.CommandTimeoutMS, policy.MaxCommandOutputBytes, policy.MaxFileBytes,
			policy.MaxPatchBytes, now.UnixMilli(), projectID, policy.Role); err != nil {
			return Settings{}, fmt.Errorf("update %s tool policy: %w", policy.Role, err)
		}
	}

	settings, err := loadSettings(ctx, tx, projectID)
	if err != nil {
		return Settings{}, err
	}
	if err := tx.Commit(); err != nil {
		return Settings{}, fmt.Errorf("commit agent settings update: %w", err)
	}
	r.record(ctx, "agent.settings_updated", map[string]any{
		"project_id": projectID,
		"version":    nextVersion,
		"roles":      []Role{RolePlanner, RoleExecutor, RoleReviewer},
	}, now)
	return settings, nil
}

func ensureDefaults(ctx context.Context, tx *sql.Tx, projectID string) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id = ?`, projectID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrProjectNotFound
		}
		return fmt.Errorf("check project for agent settings: %w", err)
	}

	now := time.Now().UTC().UnixMilli()
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO agent_settings (project_id, version, created_at, updated_at)
		VALUES (?, 1, ?, ?)
	`, projectID, now, now); err != nil {
		return fmt.Errorf("insert agent settings aggregate: %w", err)
	}
	for _, config := range defaultAgents() {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO agent_configs (
				project_id, role, provider, model, temperature, max_output_tokens,
				enabled, system_instruction, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, projectID, config.Role, config.Provider, config.Model, config.Temperature,
			config.MaxOutputTokens, boolInteger(config.Enabled), config.SystemInstruction, now, now); err != nil {
			return fmt.Errorf("insert %s default agent config: %w", config.Role, err)
		}
	}
	for _, policy := range defaultPolicies() {
		toolsJSON, _ := json.Marshal(policy.AllowedTools)
		profilesJSON, _ := json.Marshal(policy.AllowedCommandProfiles)
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO tool_policies (
				project_id, role, allowed_tools_json, allowed_command_profiles_json,
				network_access, filesystem_access, command_timeout_ms,
				max_command_output_bytes, max_file_bytes, max_patch_bytes, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, projectID, policy.Role, string(toolsJSON), string(profilesJSON), policy.NetworkAccess,
			policy.FilesystemAccess, policy.CommandTimeoutMS, policy.MaxCommandOutputBytes,
			policy.MaxFileBytes, policy.MaxPatchBytes, now, now); err != nil {
			return fmt.Errorf("insert %s default tool policy: %w", policy.Role, err)
		}
	}
	return nil
}

func loadSettings(ctx context.Context, tx *sql.Tx, projectID string) (Settings, error) {
	var settings Settings
	var createdAt, updatedAt int64
	if err := tx.QueryRowContext(ctx, `
		SELECT project_id, version, created_at, updated_at
		FROM agent_settings WHERE project_id = ?
	`, projectID).Scan(&settings.ProjectID, &settings.Version, &createdAt, &updatedAt); err != nil {
		return Settings{}, fmt.Errorf("read agent settings aggregate: %w", err)
	}
	settings.CreatedAt = time.UnixMilli(createdAt).UTC()
	settings.UpdatedAt = time.UnixMilli(updatedAt).UTC()

	rows, err := tx.QueryContext(ctx, `
		SELECT role, provider, model, temperature, max_output_tokens, enabled, system_instruction
		FROM agent_configs WHERE project_id = ?
		ORDER BY CASE role WHEN 'PLANNER' THEN 1 WHEN 'EXECUTOR' THEN 2 ELSE 3 END
	`, projectID)
	if err != nil {
		return Settings{}, fmt.Errorf("query agent configs: %w", err)
	}
	for rows.Next() {
		var config AgentConfig
		var enabled int
		if err := rows.Scan(&config.Role, &config.Provider, &config.Model, &config.Temperature,
			&config.MaxOutputTokens, &enabled, &config.SystemInstruction); err != nil {
			rows.Close()
			return Settings{}, fmt.Errorf("scan agent config: %w", err)
		}
		config.Enabled = enabled == 1
		settings.Agents = append(settings.Agents, config)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Settings{}, fmt.Errorf("iterate agent configs: %w", err)
	}
	rows.Close()

	rows, err = tx.QueryContext(ctx, `
		SELECT role, allowed_tools_json, allowed_command_profiles_json, network_access,
			filesystem_access, command_timeout_ms, max_command_output_bytes,
			max_file_bytes, max_patch_bytes
		FROM tool_policies WHERE project_id = ?
		ORDER BY CASE role WHEN 'PLANNER' THEN 1 WHEN 'EXECUTOR' THEN 2 ELSE 3 END
	`, projectID)
	if err != nil {
		return Settings{}, fmt.Errorf("query tool policies: %w", err)
	}
	for rows.Next() {
		var policy ToolPolicy
		var toolsJSON, profilesJSON string
		if err := rows.Scan(&policy.Role, &toolsJSON, &profilesJSON, &policy.NetworkAccess,
			&policy.FilesystemAccess, &policy.CommandTimeoutMS, &policy.MaxCommandOutputBytes,
			&policy.MaxFileBytes, &policy.MaxPatchBytes); err != nil {
			rows.Close()
			return Settings{}, fmt.Errorf("scan tool policy: %w", err)
		}
		if err := json.Unmarshal([]byte(toolsJSON), &policy.AllowedTools); err != nil {
			rows.Close()
			return Settings{}, fmt.Errorf("decode %s allowed tools: %w", policy.Role, err)
		}
		if err := json.Unmarshal([]byte(profilesJSON), &policy.AllowedCommandProfiles); err != nil {
			rows.Close()
			return Settings{}, fmt.Errorf("decode %s command profiles: %w", policy.Role, err)
		}
		settings.ToolPolicies = append(settings.ToolPolicies, policy)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Settings{}, fmt.Errorf("iterate tool policies: %w", err)
	}
	rows.Close()
	return settings, nil
}

func defaultAgents() []AgentConfig {
	return []AgentConfig{
		{Role: RolePlanner, Temperature: 0.1, MaxOutputTokens: 4096, Enabled: true},
		{Role: RoleExecutor, Temperature: 0.1, MaxOutputTokens: 8192, Enabled: true},
		{Role: RoleReviewer, Temperature: 0, MaxOutputTokens: 4096, Enabled: true},
	}
}

func defaultPolicies() []ToolPolicy {
	return []ToolPolicy{
		{
			Role: RolePlanner, AllowedTools: []string{}, AllowedCommandProfiles: []string{},
			NetworkAccess: NetworkDisabled, FilesystemAccess: FilesystemReadOnly,
			CommandTimeoutMS: 30000, MaxCommandOutputBytes: 262144, MaxFileBytes: 1048576, MaxPatchBytes: 1048576,
		},
		{
			Role:                   RoleExecutor,
			AllowedTools:           []string{ToolFileRead, ToolFileSearch, ToolFilePatch, ToolFileCreate, ToolFileDelete, ToolGitStatus, ToolGitDiff},
			AllowedCommandProfiles: []string{}, NetworkAccess: NetworkDisabled,
			FilesystemAccess: FilesystemWorkspaceWrite, CommandTimeoutMS: 120000,
			MaxCommandOutputBytes: 1048576, MaxFileBytes: 2097152, MaxPatchBytes: 4194304,
		},
		{
			Role: RoleReviewer, AllowedTools: []string{ToolFileRead, ToolFileSearch, ToolGitStatus, ToolGitDiff},
			AllowedCommandProfiles: []string{}, NetworkAccess: NetworkDisabled,
			FilesystemAccess: FilesystemReadOnly, CommandTimeoutMS: 30000,
			MaxCommandOutputBytes: 262144, MaxFileBytes: 2097152, MaxPatchBytes: 4194304,
		},
	}
}

func normalizeAgents(configs []AgentConfig) []AgentConfig {
	result := append([]AgentConfig(nil), configs...)
	for index := range result {
		result[index].Role = Role(strings.ToUpper(strings.TrimSpace(string(result[index].Role))))
		result[index].Provider = strings.ToLower(strings.TrimSpace(result[index].Provider))
		result[index].Model = strings.TrimSpace(result[index].Model)
		result[index].SystemInstruction = strings.TrimSpace(result[index].SystemInstruction)
	}
	slices.SortFunc(result, func(left, right AgentConfig) int {
		return roleIndex(left.Role) - roleIndex(right.Role)
	})
	return result
}

func normalizePolicies(policies []ToolPolicy) []ToolPolicy {
	result := append([]ToolPolicy(nil), policies...)
	for index := range result {
		result[index].Role = Role(strings.ToUpper(strings.TrimSpace(string(result[index].Role))))
		result[index].NetworkAccess = NetworkAccess(strings.ToUpper(strings.TrimSpace(string(result[index].NetworkAccess))))
		result[index].FilesystemAccess = FilesystemAccess(strings.ToUpper(strings.TrimSpace(string(result[index].FilesystemAccess))))
		result[index].AllowedTools = normalizeStrings(result[index].AllowedTools, true)
		result[index].AllowedCommandProfiles = normalizeStrings(result[index].AllowedCommandProfiles, true)
	}
	slices.SortFunc(result, func(left, right ToolPolicy) int {
		return roleIndex(left.Role) - roleIndex(right.Role)
	})
	return result
}

func validateAggregate(configs []AgentConfig, policies []ToolPolicy) error {
	if len(configs) != len(roles) || len(policies) != len(roles) {
		return fmt.Errorf("%w: exactly one agent config and tool policy are required for each role", ErrInvalidSettings)
	}
	for index, role := range roles {
		if configs[index].Role != role || policies[index].Role != role {
			return fmt.Errorf("%w: roles must include PLANNER, EXECUTOR, and REVIEWER exactly once", ErrInvalidSettings)
		}
		if err := validateAgent(configs[index]); err != nil {
			return err
		}
		if err := validatePolicy(policies[index]); err != nil {
			return err
		}
	}
	executor := configs[roleIndex(RoleExecutor)]
	reviewer := configs[roleIndex(RoleReviewer)]
	if executor.Enabled && reviewer.Enabled && executor.Provider != "" && reviewer.Provider != "" &&
		executor.Provider == reviewer.Provider && executor.Model == reviewer.Model {
		return fmt.Errorf("%w: executor and reviewer must not use the same explicit provider/model", ErrInvalidSettings)
	}
	return nil
}

func validateAgent(config AgentConfig) error {
	if (config.Provider == "") != (config.Model == "") {
		return fmt.Errorf("%w: %s provider and model must both be empty or both be set", ErrInvalidSettings, config.Role)
	}
	if config.Temperature < 0 || config.Temperature > 2 {
		return fmt.Errorf("%w: %s temperature must be between 0 and 2", ErrInvalidSettings, config.Role)
	}
	if config.MaxOutputTokens < 256 || config.MaxOutputTokens > 65536 {
		return fmt.Errorf("%w: %s max_output_tokens must be between 256 and 65536", ErrInvalidSettings, config.Role)
	}
	if len(config.SystemInstruction) > maxInstruction {
		return fmt.Errorf("%w: %s system instruction exceeds %d characters", ErrInvalidSettings, config.Role, maxInstruction)
	}
	return nil
}

func validatePolicy(policy ToolPolicy) error {
	if len(policy.AllowedTools) > len(knownTools) || len(policy.AllowedCommandProfiles) > 64 {
		return fmt.Errorf("%w: %s tool policy contains too many entries", ErrInvalidSettings, policy.Role)
	}
	for _, tool := range policy.AllowedTools {
		if _, exists := knownTools[tool]; !exists {
			return fmt.Errorf("%w: %s tool %q is unknown", ErrInvalidSettings, policy.Role, tool)
		}
		if policy.Role != RoleExecutor {
			if _, readOnly := readOnlyTools[tool]; !readOnly {
				return fmt.Errorf("%w: %s cannot use write or command tool %q", ErrInvalidSettings, policy.Role, tool)
			}
		}
	}
	for _, profile := range policy.AllowedCommandProfiles {
		if !commandProfilePattern.MatchString(profile) {
			return fmt.Errorf("%w: %s command profile %q is invalid", ErrInvalidSettings, policy.Role, profile)
		}
	}
	if slices.Contains(policy.AllowedTools, ToolCommandRun) && len(policy.AllowedCommandProfiles) == 0 {
		return fmt.Errorf("%w: command_run requires at least one allowed command profile", ErrInvalidSettings)
	}
	if !slices.Contains(policy.AllowedTools, ToolCommandRun) && len(policy.AllowedCommandProfiles) > 0 {
		return fmt.Errorf("%w: command profiles require command_run", ErrInvalidSettings)
	}
	if policy.Role != RoleExecutor && policy.NetworkAccess != NetworkDisabled {
		return fmt.Errorf("%w: %s network access must remain disabled", ErrInvalidSettings, policy.Role)
	}
	if policy.NetworkAccess != NetworkDisabled && policy.NetworkAccess != NetworkLoopback && policy.NetworkAccess != NetworkOutbound {
		return fmt.Errorf("%w: %s network_access is invalid", ErrInvalidSettings, policy.Role)
	}
	if policy.FilesystemAccess != FilesystemReadOnly && policy.FilesystemAccess != FilesystemWorkspaceWrite {
		return fmt.Errorf("%w: %s filesystem_access is invalid", ErrInvalidSettings, policy.Role)
	}
	if policy.Role != RoleExecutor && policy.FilesystemAccess != FilesystemReadOnly {
		return fmt.Errorf("%w: %s filesystem access must remain read-only", ErrInvalidSettings, policy.Role)
	}
	if policy.CommandTimeoutMS < 1000 || policy.CommandTimeoutMS > 600000 {
		return fmt.Errorf("%w: %s command_timeout_ms must be between 1000 and 600000", ErrInvalidSettings, policy.Role)
	}
	for name, value := range map[string]int{
		"max_command_output_bytes": policy.MaxCommandOutputBytes,
		"max_file_bytes":           policy.MaxFileBytes,
		"max_patch_bytes":          policy.MaxPatchBytes,
	} {
		if value < 1024 || value > 32*1024*1024 {
			return fmt.Errorf("%w: %s %s must be between 1024 and 33554432", ErrInvalidSettings, policy.Role, name)
		}
	}
	return nil
}

func normalizeStrings(values []string, lower bool) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if lower {
			value = strings.ToLower(value)
		}
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func roleIndex(role Role) int {
	for index, candidate := range roles {
		if role == candidate {
			return index
		}
	}
	return len(roles)
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (r *Repository) record(ctx context.Context, eventType string, payload any, occurredAt time.Time) {
	if r.recorder == nil {
		return
	}
	_ = r.recorder.Record(ctx, "", eventType, payload, occurredAt)
}
