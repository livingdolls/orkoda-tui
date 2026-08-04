package publication

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/livingdolls/orkoda-tui/internal/credentials"
)

var ErrRemoteUnavailable = errors.New("remote publication is unavailable")

type CredentialStore interface {
	Get(context.Context, string) (string, error)
}

type RemotePublishInput struct {
	RepositoryURL     string
	WorkspacePath     string
	Branch            string
	TargetBranch      string
	Title             string
	Body              string
	Account           string
	Draft             bool
	CreatePullRequest bool
}

type RemotePublishResult struct {
	Provider       string `json:"provider"`
	Branch         string `json:"branch"`
	TargetBranch   string `json:"target_branch"`
	PullRequestURL string `json:"pull_request_url,omitempty"`
	PullRequestID  int    `json:"pull_request_id,omitempty"`
}

type RemotePublisher interface {
	Publish(context.Context, RemotePublishInput) (RemotePublishResult, error)
}

type GitHubPublisher struct {
	credentials CredentialStore
	client      *http.Client
	git         GitPusher
	apiBaseURL  string
}

type GitPusher interface {
	Push(context.Context, string, string, string, string) error
}

func NewGitHubPublisher(store CredentialStore) (*GitHubPublisher, error) {
	if store == nil {
		return nil, fmt.Errorf("credential store is required")
	}
	return &GitHubPublisher{
		credentials: store,
		client:      http.DefaultClient,
		git:         commandGitPusher{},
		apiBaseURL:  "https://api.github.com",
	}, nil
}

func (p *GitHubPublisher) Publish(ctx context.Context, input RemotePublishInput) (RemotePublishResult, error) {
	if p == nil || p.credentials == nil || p.git == nil {
		return RemotePublishResult{}, ErrRemoteUnavailable
	}
	input.RepositoryURL = strings.TrimSpace(input.RepositoryURL)
	input.WorkspacePath = filepath.Clean(strings.TrimSpace(input.WorkspacePath))
	input.Branch = strings.TrimSpace(input.Branch)
	input.TargetBranch = strings.TrimSpace(input.TargetBranch)
	input.Account = strings.TrimSpace(input.Account)
	if input.Account == "" {
		input.Account = "github"
	}
	if input.TargetBranch == "" {
		input.TargetBranch = "main"
	}
	if input.Title == "" {
		input.Title = "Orkoda changes"
	}
	if input.RepositoryURL == "" || input.WorkspacePath == "." || !validBranch(input.Branch) || !validBranch(input.TargetBranch) {
		return RemotePublishResult{}, fmt.Errorf("invalid GitHub publication input")
	}
	owner, repository, pushURL, err := parseGitHubRemote(input.RepositoryURL)
	if err != nil {
		return RemotePublishResult{}, err
	}
	token, err := p.credentials.Get(ctx, input.Account)
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) || errors.Is(err, credentials.ErrUnavailable) {
			return RemotePublishResult{}, fmt.Errorf("%w: GitHub credential is unavailable", ErrRemoteUnavailable)
		}
		return RemotePublishResult{}, err
	}
	if err := p.git.Push(ctx, input.WorkspacePath, pushURL, input.Branch, token); err != nil {
		return RemotePublishResult{}, fmt.Errorf("push GitHub branch: %w", err)
	}
	result := RemotePublishResult{Provider: "github", Branch: input.Branch, TargetBranch: input.TargetBranch}
	if !input.CreatePullRequest {
		return result, nil
	}
	result.PullRequestURL, result.PullRequestID, err = p.ensureDraftPullRequest(ctx, token, owner, repository, input)
	if err != nil {
		return RemotePublishResult{}, err
	}
	return result, nil
}

func (p *GitHubPublisher) ensureDraftPullRequest(ctx context.Context, token, owner, repository string, input RemotePublishInput) (string, int, error) {
	baseURL := strings.TrimRight(p.apiBaseURL, "/")
	listURL := fmt.Sprintf("%s/repos/%s/%s/pulls?state=open&head=%s:%s", baseURL, owner, repository, owner, input.Branch)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return "", 0, fmt.Errorf("create GitHub pull request lookup: %w", err)
	}
	setGitHubHeaders(request, token)
	response, err := p.client.Do(request)
	if err != nil {
		return "", 0, fmt.Errorf("lookup GitHub pull request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return "", 0, githubResponseError("lookup pull request", response)
	}
	var existing []pullRequestResponse
	if err := json.NewDecoder(response.Body).Decode(&existing); err != nil {
		return "", 0, fmt.Errorf("decode GitHub pull request lookup: %w", err)
	}
	if len(existing) > 0 {
		return existing[0].HTMLURL, existing[0].Number, nil
	}
	payload, err := json.Marshal(map[string]any{
		"title": input.Title, "body": input.Body, "head": input.Branch,
		"base": input.TargetBranch, "draft": true,
	})
	if err != nil {
		return "", 0, fmt.Errorf("encode GitHub pull request: %w", err)
	}
	request, err = http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/repos/%s/%s/pulls", baseURL, owner, repository), bytes.NewReader(payload))
	if err != nil {
		return "", 0, fmt.Errorf("create GitHub pull request request: %w", err)
	}
	setGitHubHeaders(request, token)
	request.Header.Set("Content-Type", "application/json")
	response, err = p.client.Do(request)
	if err != nil {
		return "", 0, fmt.Errorf("create GitHub pull request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return "", 0, githubResponseError("create pull request", response)
	}
	var created pullRequestResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		return "", 0, fmt.Errorf("decode GitHub pull request: %w", err)
	}
	return created.HTMLURL, created.Number, nil
}

type pullRequestResponse struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}

func setGitHubHeaders(request *http.Request, token string) {
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
}

func githubResponseError(operation string, response *http.Response) error {
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return fmt.Errorf("GitHub %s returned HTTP %d", operation, response.StatusCode)
}

var validBranchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,200}$`)

func validBranch(branch string) bool {
	return validBranchPattern.MatchString(branch) && !strings.Contains(branch, "..") && !strings.Contains(branch, "//") && !strings.HasSuffix(branch, "/")
}

func parseGitHubRemote(remote string) (owner, repository, pushURL string, err error) {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, ".git")
	if strings.HasPrefix(remote, "git@github.com:") {
		remote = strings.TrimPrefix(remote, "git@github.com:")
	} else if strings.HasPrefix(remote, "https://github.com/") {
		remote = strings.TrimPrefix(remote, "https://github.com/")
	} else if strings.HasPrefix(remote, "http://github.com/") {
		remote = strings.TrimPrefix(remote, "http://github.com/")
	} else {
		return "", "", "", fmt.Errorf("remote is not a supported GitHub URL")
	}
	parts := strings.Split(strings.Trim(remote, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(parts[0]+parts[1], " \t\r\n") {
		return "", "", "", fmt.Errorf("invalid GitHub repository URL")
	}
	return parts[0], parts[1], "https://github.com/" + parts[0] + "/" + parts[1] + ".git", nil
}

type commandGitPusher struct{}

func (commandGitPusher) Push(ctx context.Context, workspacePath, remoteURL, branch, token string) error {
	if strings.TrimSpace(token) == "" {
		return ErrRemoteUnavailable
	}
	script, err := os.CreateTemp("", "orkoda-git-askpass-*")
	if err != nil {
		return fmt.Errorf("create ephemeral Git credential helper: %w", err)
	}
	scriptPath := script.Name()
	defer os.Remove(scriptPath)
	if err := script.Chmod(0o700); err != nil {
		script.Close()
		return err
	}
	if _, err := script.WriteString("#!/bin/sh\ncase \"$1\" in *Username*) printf '%s\\n' x-access-token ;; *) printf '%s\\n' \"$GITHUB_TOKEN\" ;; esac\n"); err != nil {
		script.Close()
		return err
	}
	if err := script.Close(); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "git", "-C", workspacePath, "push", remoteURL, "HEAD:refs/heads/"+branch)
	// Keep the child environment intentionally small. In particular, do not
	// inherit a process-wide GITHUB_TOKEN or unrelated secrets when the
	// ephemeral askpass helper is invoked.
	command.Env = []string{
		"PATH=" + safePath(), "HOME=/tmp/orkoda-home", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=1", "GIT_ASKPASS=" + scriptPath, "GITHUB_TOKEN=" + token, "LC_ALL=C",
	}
	if output, err := command.CombinedOutput(); err != nil {
		_ = output
		return fmt.Errorf("git push failed")
	}
	return nil
}

func safePath() string {
	if value := strings.TrimSpace(os.Getenv("PATH")); value != "" {
		return value
	}
	return "/usr/bin:/bin"
}

var _ RemotePublisher = (*GitHubPublisher)(nil)
