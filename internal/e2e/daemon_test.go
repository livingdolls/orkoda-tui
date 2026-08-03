//go:build !windows

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const e2eToken = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

type apiClient struct {
	baseURL string
	token   string
	http    *http.Client
}

type apiEnvelope[T any] struct {
	Data T `json:"data"`
}

type projectResponse struct {
	ID           string            `json:"id"`
	Repositories []repositoryEntry `json:"repositories"`
}

type repositoryEntry struct {
	ID string `json:"id"`
}

type planResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type workflowJobResponse struct {
	ID               string `json:"id"`
	BaseCommitSHA    string `json:"base_commit_sha"`
	Status           string `json:"status"`
	Version          int    `json:"version"`
	ExecutionVersion int    `json:"execution_version"`
	FailureCode      string `json:"failure_code"`
	FailureMessage   string `json:"failure_message"`
}

type executionResponse struct {
	ID            string `json:"id"`
	BaseCommitSHA string `json:"base_commit_sha"`
	Status        string `json:"status"`
}

type checkpointResponse struct {
	PatchChecksum string `json:"patch_checksum"`
}

type checkRunResponse struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	PassedSteps int    `json:"passed_steps"`
	FailedSteps int    `json:"failed_steps"`
}

type checkStepResponse struct {
	Status string `json:"status"`
}

type reviewResponse struct {
	Status  string `json:"status"`
	Verdict string `json:"verdict"`
}

type workspaceResponse struct {
	Path          string `json:"path"`
	BaseCommitSHA string `json:"base_commit_sha"`
	HeadSHA       string `json:"head_sha"`
	Status        string `json:"status"`
	Dirty         bool   `json:"dirty"`
}

type transitionResponse struct {
	Action   string `json:"action"`
	ToStatus string `json:"to_status"`
}

func TestDaemonWorkflowEndToEnd(t *testing.T) {
	if value := strings.ToLower(strings.TrimSpace(os.Getenv("ORKODA_E2E"))); value != "1" && value != "true" {
		t.Skip("set ORKODA_E2E=1 to run the daemon end-to-end test")
	}

	testRoot := t.TempDir()
	repositoryRoot := filepath.Join(testRoot, "repository")
	stateRoot := filepath.Join(testRoot, "state")
	if err := os.MkdirAll(repositoryRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	createFixtureRepository(t, repositoryRoot)

	goBinary := resolveGoBinary(t)
	apiBinary := filepath.Join(stateRoot, "orkoda-api")
	buildContext, cancelBuild := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelBuild()
	build := exec.CommandContext(buildContext, goBinary, "build", "-o", apiBinary, "./cmd/api")
	build.Dir = repositoryRootForSource(t)
	build.Env = testEnvironment(map[string]string{
		"PATH": prependPath(filepath.Dir(goBinary), os.Getenv("PATH")),
	})
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build API daemon: %v\n%s", err, output)
	}

	port := unusedTCPPort(t)
	pathEnv := prependPath(filepath.Dir(goBinary), os.Getenv("PATH"))
	daemonOutput := &bytes.Buffer{}
	daemon := exec.Command(apiBinary)
	daemon.Dir = repositoryRootForSource(t)
	daemon.Env = testEnvironment(map[string]string{
		"ORKODA_ENV":                      "test",
		"ORKODA_API_HOST":                 "127.0.0.1",
		"ORKODA_API_PORT":                 strconv.Itoa(port),
		"ORKODA_DATA_DIR":                 stateRoot,
		"ORKODA_API_TOKEN":                e2eToken,
		"ORKODA_API_TOKEN_FILE":           filepath.Join(stateRoot, "api.token"),
		"ORKODA_SANDBOX_MODE":             "host",
		"ORKODA_ALLOW_UNSANDBOXED_CHECKS": "true",
		"ORKODA_WORKSPACE_LEASE_TTL":      "30s",
		"ORKODA_SHUTDOWN_TIMEOUT":         "5s",
		"ORKODA_LLM_PROVIDER":             "local-fake",
		"PATH":                            pathEnv,
	})
	daemon.Stdout = daemonOutput
	daemon.Stderr = daemonOutput
	if err := daemon.Start(); err != nil {
		t.Fatalf("start API daemon: %v", err)
	}
	daemonDone := make(chan error, 1)
	go func() { daemonDone <- daemon.Wait() }()
	t.Cleanup(func() {
		select {
		case <-daemonDone:
			return
		default:
		}
		_ = daemon.Process.Signal(os.Interrupt)
		select {
		case <-daemonDone:
		case <-time.After(8 * time.Second):
			_ = daemon.Process.Kill()
			<-daemonDone
		}
	})

	client := apiClient{
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		token:   e2eToken,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
	healthContext, cancelHealth := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelHealth()
	if err := client.waitForHealth(healthContext); err != nil {
		t.Fatalf("wait for API health: %v\n%s", err, daemonOutput.String())
	}

	workflowContext, cancelWorkflow := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancelWorkflow()
	var project projectResponse
	if err := client.do(workflowContext, http.MethodPost, "/api/v1/projects", map[string]any{
		"name":            "E2E fixture",
		"repository_path": repositoryRoot,
	}, &project); err != nil {
		t.Fatal(err)
	}
	if project.ID == "" || len(project.Repositories) != 1 || project.Repositories[0].ID == "" {
		t.Fatalf("unexpected project response: %#v", project)
	}

	var plan planResponse
	if err := client.do(workflowContext, http.MethodPost, "/api/v1/projects/"+project.ID+"/plans", map[string]any{
		"title":               "Run the deterministic E2E workflow",
		"requirement":         "Verify the complete execution, check, review, approval, and publication path.",
		"acceptance_criteria": []string{"The workflow reaches COMPLETED and publishes a clean commit."},
	}, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.ID == "" || plan.Status != "DRAFT" {
		t.Fatalf("unexpected draft plan response: %#v", plan)
	}
	if err := client.do(workflowContext, http.MethodPatch, "/api/v1/plans/"+plan.ID, map[string]any{
		"title":  "Run the deterministic E2E workflow",
		"status": "READY",
	}, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Status != "READY" {
		t.Fatalf("plan did not become READY: %#v", plan)
	}

	var job workflowJobResponse
	if err := client.do(workflowContext, http.MethodPost, "/api/v1/projects/"+project.ID+"/jobs", map[string]any{
		"plan_id":       plan.ID,
		"repository_id": project.Repositories[0].ID,
		"base_branch":   "main",
	}, &job); err != nil {
		t.Fatal(err)
	}
	if job.ID == "" || job.Status != "READY" || job.Version != 1 {
		t.Fatalf("unexpected created job response: %#v", job)
	}
	if err := client.do(workflowContext, http.MethodPost, "/api/v1/jobs/"+job.ID+"/start", map[string]any{
		"expected_version": job.Version,
		"details":          map[string]any{"source": "daemon-e2e"},
	}, &job); err != nil {
		t.Fatal(err)
	}
	if job.Status != "WORKSPACE_PREPARING" {
		t.Fatalf("job did not start: %#v", job)
	}

	job, err := waitForJob(t, workflowContext, client, job.ID, "WAITING_FOR_APPROVAL")
	if err != nil {
		t.Fatalf("%v\n%s", err, daemonOutput.String())
	}
	if job.ExecutionVersion != 1 {
		t.Fatalf("unexpected execution version: %#v", job)
	}

	var executions []executionResponse
	if err := client.do(workflowContext, http.MethodGet, "/api/v1/jobs/"+job.ID+"/executions", nil, &executions); err != nil {
		t.Fatal(err)
	}
	if len(executions) != 1 || executions[0].Status != "COMPLETED" {
		t.Fatalf("unexpected executions: %#v", executions)
	}
	execution := executions[0]
	var checkpoints []checkpointResponse
	if err := client.do(workflowContext, http.MethodGet, "/api/v1/executions/"+execution.ID+"/checkpoints", nil, &checkpoints); err != nil {
		t.Fatal(err)
	}
	if len(checkpoints) != 1 || checkpoints[0].PatchChecksum == "" {
		t.Fatalf("unexpected checkpoints: %#v", checkpoints)
	}

	var checkRuns []checkRunResponse
	if err := client.do(workflowContext, http.MethodGet, "/api/v1/jobs/"+job.ID+"/checks", nil, &checkRuns); err != nil {
		t.Fatal(err)
	}
	if len(checkRuns) != 1 || checkRuns[0].Status != "PASSED" || checkRuns[0].FailedSteps != 0 {
		t.Fatalf("unexpected check runs: %#v", checkRuns)
	}
	var checkSteps []checkStepResponse
	if err := client.do(workflowContext, http.MethodGet, "/api/v1/checks/"+checkRuns[0].ID+"/steps", nil, &checkSteps); err != nil {
		t.Fatal(err)
	}
	if len(checkSteps) == 0 {
		t.Fatal("check run did not persist any steps")
	}
	for _, step := range checkSteps {
		if step.Status != "PASSED" {
			t.Fatalf("check step did not pass: %#v", checkSteps)
		}
	}

	var reviews []reviewResponse
	if err := client.do(workflowContext, http.MethodGet, "/api/v1/jobs/"+job.ID+"/reviews", nil, &reviews); err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 1 || reviews[0].Status != "COMPLETED" || reviews[0].Verdict != "APPROVE" {
		t.Fatalf("unexpected reviews: %#v", reviews)
	}

	if err := client.do(workflowContext, http.MethodPost, "/api/v1/jobs/"+job.ID+"/approve", map[string]any{
		"expected_version":  job.Version,
		"execution_version": job.ExecutionVersion,
		"base_commit_sha":   execution.BaseCommitSHA,
		"patch_checksum":    checkpoints[0].PatchChecksum,
		"note":              "E2E approval",
		"review_override":   false,
	}, nil); err != nil {
		t.Fatal(err)
	}
	job, err = getJob(workflowContext, client, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "APPROVED" {
		t.Fatalf("job was not approved: %#v", job)
	}

	if err := client.do(workflowContext, http.MethodPost, "/api/v1/jobs/"+job.ID+"/publish", map[string]any{
		"expected_version": job.Version,
		"details":          map[string]any{"source": "daemon-e2e"},
	}, nil); err != nil {
		t.Fatal(err)
	}
	job, err = waitForJob(t, workflowContext, client, job.ID, "COMPLETED")
	if err != nil {
		t.Fatalf("%v\n%s", err, daemonOutput.String())
	}

	var workspace workspaceResponse
	if err := client.do(workflowContext, http.MethodGet, "/api/v1/jobs/"+job.ID+"/workspace", nil, &workspace); err != nil {
		t.Fatal(err)
	}
	if workspace.Path == "" || workspace.Status != "READY" || workspace.Dirty {
		t.Fatalf("unexpected published workspace: %#v", workspace)
	}
	if workspace.BaseCommitSHA != job.BaseCommitSHA || workspace.HeadSHA == "" || workspace.HeadSHA == job.BaseCommitSHA {
		t.Fatalf("workspace commit snapshot is invalid: %#v", workspace)
	}
	if status := runGitOutput(t, workspace.Path, "status", "--porcelain"); status != "" {
		t.Fatalf("published workspace is dirty: %q", status)
	}
	if subject := runGitOutput(t, workspace.Path, "log", "-1", "--pretty=%s"); subject != "Orkoda publish: "+job.ID {
		t.Fatalf("published commit subject = %q", subject)
	}

	var transitions []transitionResponse
	if err := client.do(workflowContext, http.MethodGet, "/api/v1/jobs/"+job.ID+"/transitions", nil, &transitions); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"WORKSPACE_PREPARING", "QUEUED", "EXECUTING", "CHECKING", "REVIEWING",
		"WAITING_FOR_APPROVAL", "APPROVED", "PUBLISHING", "COMPLETED",
	} {
		if !transitionContains(transitions, expected) {
			t.Fatalf("transition %s missing from %#v", expected, transitions)
		}
	}
	t.Logf("E2E passed: job=%s execution=%s checks=%d published_head=%s", job.ID, execution.ID, len(checkSteps), workspace.HeadSHA)
}

func (c apiClient) do(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode %s %s request: %w", method, path, err)
		}
		body = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create %s %s request: %w", method, path, err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("call %s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("read %s %s response: %w", method, path, err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("call %s %s returned %d: %s", method, path, response.StatusCode, strings.TrimSpace(string(payload)))
	}
	if output == nil {
		return nil
	}
	var envelope apiEnvelope[json.RawMessage]
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("decode %s %s response: %w", method, path, err)
	}
	if len(envelope.Data) == 0 {
		return fmt.Errorf("decode %s %s response: data is missing", method, path)
	}
	if err := json.Unmarshal(envelope.Data, output); err != nil {
		return fmt.Errorf("decode %s %s data: %w", method, path, err)
	}
	return nil
}

func (c apiClient) waitForHealth(ctx context.Context) error {
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health/live", nil)
		if err != nil {
			return err
		}
		response, requestErr := c.http.Do(request)
		if requestErr == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func getJob(ctx context.Context, client apiClient, id string) (workflowJobResponse, error) {
	var job workflowJobResponse
	if err := client.do(ctx, http.MethodGet, "/api/v1/jobs/"+id, nil, &job); err != nil {
		return workflowJobResponse{}, err
	}
	return job, nil
}

func waitForJob(t *testing.T, ctx context.Context, client apiClient, id, wanted string) (workflowJobResponse, error) {
	t.Helper()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	lastStatus := ""
	for {
		job, err := getJob(ctx, client, id)
		if err != nil {
			return workflowJobResponse{}, err
		}
		if job.Status != lastStatus {
			t.Logf("job %s -> %s (version %d)", job.ID, job.Status, job.Version)
			lastStatus = job.Status
		}
		if job.Status == wanted {
			return job, nil
		}
		switch job.Status {
		case "FAILED", "CANCELLED", "REJECTED":
			return workflowJobResponse{}, fmt.Errorf(
				"job reached unexpected terminal status %s (code=%s message=%s): %#v",
				job.Status, job.FailureCode, job.FailureMessage, job,
			)
		}
		select {
		case <-ctx.Done():
			return workflowJobResponse{}, fmt.Errorf("waiting for job %s to reach %s: %w", id, wanted, ctx.Err())
		case <-ticker.C:
		}
	}
}

func transitionContains(transitions []transitionResponse, status string) bool {
	for _, transition := range transitions {
		if transition.ToStatus == status {
			return true
		}
	}
	return false
}

func createFixtureRepository(t *testing.T, root string) {
	t.Helper()
	writeFixtureFile(t, filepath.Join(root, "go.mod"), "module example.com/orkoda-e2e\n\ngo 1.26\n")
	writeFixtureFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")
	runGit(t, root, "init")
	runGit(t, root, "checkout", "-b", "main")
	runGit(t, root, "config", "user.name", "Orkoda E2E")
	runGit(t, root, "config", "user.email", "e2e@localhost")
	runGit(t, root, "add", "--all")
	runGit(t, root, "commit", "--no-gpg-sign", "-m", "Initial E2E fixture")
}

func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	command.Env = testEnvironment(nil)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func runGitOutput(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	return runGit(t, root, arguments...)
}

func resolveGoBinary(t *testing.T) string {
	t.Helper()
	if configured := strings.TrimSpace(os.Getenv("ORKODA_E2E_GO_BIN")); configured != "" {
		return configured
	}
	candidate := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	path, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("locate Go binary: %v", err)
	}
	return path
}

func repositoryRootForSource(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join(workingDirectory, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func unusedTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func prependPath(directory, path string) string {
	if path == "" {
		return directory
	}
	return directory + string(os.PathListSeparator) + path
}

func testEnvironment(overrides map[string]string) []string {
	values := make(map[string]string)
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	for key, value := range overrides {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	return environment
}
