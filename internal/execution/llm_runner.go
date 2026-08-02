package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/livingdolls/orkoda-tui/internal/llm"
)

const (
	defaultExecutorIterations = 16
	maxToolResultBytes        = 16 * 1024
)

type LLMRunner struct {
	gateway       llm.Gateway
	selector      *ContextSelector
	repository    *Repository
	maxIterations int
}

func NewLLMRunner(gateway llm.Gateway, selector *ContextSelector, repository *Repository) (*LLMRunner, error) {
	if gateway == nil || selector == nil || repository == nil {
		return nil, fmt.Errorf("LLM gateway, context selector, and execution repository are required")
	}
	return &LLMRunner{
		gateway: gateway, selector: selector, repository: repository,
		maxIterations: defaultExecutorIterations,
	}, nil
}

type executorAction struct {
	Type      string            `json:"type"`
	Tool      string            `json:"tool,omitempty"`
	Arguments executorArguments `json:"arguments,omitempty"`
	Summary   string            `json:"summary"`
}

type executorArguments struct {
	Path        string `json:"path,omitempty"`
	Query       string `json:"query,omitempty"`
	Content     string `json:"content,omitempty"`
	Expected    string `json:"expected,omitempty"`
	Replacement string `json:"replacement,omitempty"`
	MaxResults  int    `json:"max_results,omitempty"`
}

func (r *LLMRunner) Run(ctx context.Context, run RunContext) error {
	if err := r.repository.RecoverRunningIterations(ctx, run.Execution.ID); err != nil {
		return fmt.Errorf("recover executor iterations: %w", err)
	}
	gitStatus, err := run.Tools.toolset.GitStatus(ctx)
	if err != nil {
		return fmt.Errorf("read initial Git status: %w", err)
	}
	selected, err := r.selector.Select(ctx, run.Execution, run.Workspace.Path, gitStatus)
	if err != nil {
		return err
	}
	contextJSON, err := json.Marshal(selected)
	if err != nil {
		return fmt.Errorf("marshal executor context: %w", err)
	}

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: executorSystemPrompt},
		{Role: llm.RoleUser, Content: "Implement the approved plan using only the allowed tools. Execution context:\n" + string(contextJSON)},
	}

	for index := 0; index < r.maxIterations; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		response, err := r.gateway.Complete(ctx, run.Execution.Provider, llm.Request{
			Model:           run.Execution.Model,
			Messages:        messages,
			ResponseSchema:  json.RawMessage(executorActionSchema),
			MaxOutputTokens: 4096,
			Temperature:     0.1,
			Metadata: map[string]string{
				"agent_role": "executor", "execution_id": run.Execution.ID,
				"executor_iteration": fmt.Sprintf("%d", index+1),
			},
		})
		if err != nil {
			return fmt.Errorf("executor LLM iteration %d: %w", index+1, err)
		}
		var action executorAction
		if err := json.Unmarshal([]byte(response.Content), &action); err != nil {
			return fmt.Errorf("decode executor action: %w", err)
		}
		action.Type = strings.ToLower(strings.TrimSpace(action.Type))
		action.Tool = strings.TrimSpace(action.Tool)
		action.Summary = strings.TrimSpace(action.Summary)
		if err := validateExecutorAction(action); err != nil {
			return err
		}

		iteration, err := r.repository.BeginIteration(ctx, run.Execution.ID, IterationInput{
			Provider:      response.Usage.FinalProvider,
			Model:         response.Usage.FinalModel,
			ActionType:    action.Type,
			Tool:          action.Tool,
			ActionSummary: safeActionSummary(action),
			Usage:         response.Usage,
		})
		if err != nil {
			return err
		}
		if iteration.Provider == "" {
			iteration.Provider = run.Execution.Provider
		}
		if iteration.Model == "" {
			iteration.Model = run.Execution.Model
		}

		if action.Type == "finish" {
			if err := r.repository.CompleteIteration(ctx, iteration.ID, map[string]any{"summary": action.Summary}); err != nil {
				return err
			}
			return nil
		}

		result, summary, toolErr := executeAgentTool(ctx, run.Tools, action)
		if toolErr != nil {
			_ = r.repository.FailIteration(context.WithoutCancel(ctx), iteration.ID, toolErrorCode(toolErr), toolErr.Error())
			messages = append(messages,
				llm.Message{Role: llm.RoleAssistant, Content: response.Content},
				llm.Message{Role: llm.RoleUser, Content: "Tool failed safely: " + boundText(toolErr.Error(), 2048) + ". Choose a corrected action."},
			)
			continue
		}
		if err := r.repository.CompleteIteration(ctx, iteration.ID, summary); err != nil {
			return err
		}
		messages = append(messages,
			llm.Message{Role: llm.RoleAssistant, Content: response.Content},
			llm.Message{Role: llm.RoleUser, Content: "Tool result:\n" + boundText(result, maxToolResultBytes)},
		)
	}
	return fmt.Errorf("executor reached maximum iteration count %d", r.maxIterations)
}

func executeAgentTool(ctx context.Context, tools *RecordedTools, action executorAction) (string, map[string]any, error) {
	arguments := action.Arguments
	switch action.Tool {
	case "file_read":
		value, err := tools.Read(ctx, arguments.Path)
		return value, map[string]any{"path": arguments.Path, "bytes": len([]byte(value))}, err
	case "file_search":
		value, err := tools.Search(ctx, arguments.Query, arguments.MaxResults)
		payload, _ := json.Marshal(value)
		return string(payload), map[string]any{"query": arguments.Query, "matches": len(value)}, err
	case "file_create":
		err := tools.Create(ctx, arguments.Path, arguments.Content)
		return "created " + arguments.Path, map[string]any{"path": arguments.Path, "bytes": len([]byte(arguments.Content))}, err
	case "file_patch":
		err := tools.Patch(ctx, arguments.Path, arguments.Expected, arguments.Replacement)
		return "patched " + arguments.Path, map[string]any{
			"path": arguments.Path, "expected_bytes": len([]byte(arguments.Expected)),
			"replacement_bytes": len([]byte(arguments.Replacement)),
		}, err
	case "file_delete":
		err := tools.Delete(ctx, arguments.Path)
		return "deleted " + arguments.Path, map[string]any{"path": arguments.Path}, err
	case "git_status":
		value, err := tools.GitStatus(ctx)
		return value, map[string]any{"bytes": len([]byte(value))}, err
	case "git_diff":
		value, err := tools.GitDiff(ctx)
		return value, map[string]any{"bytes": len([]byte(value))}, err
	default:
		return "", nil, fmt.Errorf("unsupported executor tool %q", action.Tool)
	}
}

func validateExecutorAction(action executorAction) error {
	if action.Type == "finish" {
		if action.Summary == "" {
			return fmt.Errorf("executor finish action requires a summary")
		}
		return nil
	}
	if action.Type != "tool" || action.Tool == "" {
		return fmt.Errorf("executor action must be tool or finish")
	}
	switch action.Tool {
	case "file_read", "file_create", "file_patch", "file_delete":
		if strings.TrimSpace(action.Arguments.Path) == "" {
			return fmt.Errorf("executor tool %s requires path", action.Tool)
		}
	case "file_search":
		if strings.TrimSpace(action.Arguments.Query) == "" {
			return fmt.Errorf("file_search requires query")
		}
	case "git_status", "git_diff":
	default:
		return fmt.Errorf("unsupported executor tool %q", action.Tool)
	}
	return nil
}

func safeActionSummary(action executorAction) map[string]any {
	summary := map[string]any{"type": action.Type, "tool": action.Tool, "summary": boundText(action.Summary, 512)}
	if action.Arguments.Path != "" {
		summary["path"] = action.Arguments.Path
	}
	if action.Arguments.Query != "" {
		summary["query"] = boundText(action.Arguments.Query, 256)
	}
	if action.Arguments.Content != "" {
		summary["content_bytes"] = len([]byte(action.Arguments.Content))
	}
	if action.Arguments.Expected != "" || action.Arguments.Replacement != "" {
		summary["expected_bytes"] = len([]byte(action.Arguments.Expected))
		summary["replacement_bytes"] = len([]byte(action.Arguments.Replacement))
	}
	return summary
}

func toolErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrToolNotAllowed):
		return "TOOL_NOT_ALLOWED"
	case errors.Is(err, ErrUnsafePath):
		return "UNSAFE_PATH"
	case errors.Is(err, ErrSizeLimit):
		return "SIZE_LIMIT"
	case errors.Is(err, ErrToolCallLimit):
		return "TOOL_CALL_LIMIT"
	default:
		return "TOOL_FAILED"
	}
}

const executorSystemPrompt = `You are the Orkoda Executor Agent. Modify only the isolated workspace through the provided tools. Follow the requirement, acceptance criteria, constraints, and repository conventions. Inspect before editing. Prefer small exact patches. Never access credentials, .git internals, absolute paths, parent paths, network resources, or shell commands. Return exactly one JSON action per response. Use finish only after reviewing git_status and git_diff and when the implementation is complete.`

const executorActionSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["type", "summary"],
  "properties": {
    "type": {"type": "string", "enum": ["tool", "finish"]},
    "tool": {"type": "string", "enum": ["file_read", "file_search", "file_create", "file_patch", "file_delete", "git_status", "git_diff"]},
    "summary": {"type": "string", "minLength": 1, "maxLength": 1000},
    "arguments": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "path": {"type": "string", "maxLength": 1024},
        "query": {"type": "string", "maxLength": 512},
        "content": {"type": "string"},
        "expected": {"type": "string"},
        "replacement": {"type": "string"},
        "max_results": {"type": "integer", "minimum": 1, "maximum": 200}
      }
    }
  }
}`
