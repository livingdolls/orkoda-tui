package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/agentconfig"
	"github.com/livingdolls/orkoda-tui/internal/approval"
	"github.com/livingdolls/orkoda-tui/internal/checks"
	"github.com/livingdolls/orkoda-tui/internal/execution"
	"github.com/livingdolls/orkoda-tui/internal/plans"
	"github.com/livingdolls/orkoda-tui/internal/reviewer"
	"github.com/livingdolls/orkoda-tui/internal/workflowjob"
)

type reviewAgentAssignment struct {
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	SettingsVersion int    `json:"settings_version,omitempty"`
}

type reviewBoardCard struct {
	Plan           plans.Plan            `json:"plan"`
	Workflow       workflowjob.Job        `json:"workflow"`
	Execution      *execution.Execution   `json:"execution,omitempty"`
	Check          *checks.Run            `json:"check,omitempty"`
	Review         *reviewer.Run          `json:"review,omitempty"`
	Issues         []reviewer.Issue       `json:"issues"`
	PreviousReview *reviewer.Run          `json:"previous_review,omitempty"`
	PreviousIssues []reviewer.Issue       `json:"previous_issues"`
	Executor       reviewAgentAssignment  `json:"executor"`
	Reviewer       reviewAgentAssignment  `json:"reviewer"`
	Column         string                 `json:"review_column"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

type reviewBoardResponse struct {
	Cards []reviewBoardCard `json:"cards"`
}

type reviewSnapshotResponse struct {
	Card        reviewBoardCard       `json:"card"`
	CheckSteps  []checks.Step          `json:"check_steps"`
	Checkpoints []execution.Checkpoint `json:"checkpoints"`
	Decisions   []approval.Decision    `json:"decisions"`
}

func registerReviewBoardRoutes(
	api *gin.RouterGroup,
	plansRegistry PlanRegistry,
	jobsRegistry WorkflowJobRegistry,
	settingsRegistry AgentSettingsRegistry,
	executionsRegistry ExecutionRegistry,
	checksRegistry CheckRegistry,
	reviewsRegistry ReviewRegistry,
	approvalsRegistry ApprovalRegistry,
) {
	api.GET("/projects/:projectID/review-board", func(c *gin.Context) {
		if !requireReviewBoardRegistries(c, plansRegistry, jobsRegistry, executionsRegistry, checksRegistry, reviewsRegistry) {
			return
		}
		ctx := c.Request.Context()
		projectID := c.Param("projectID")
		projectPlans, err := plansRegistry.ListProject(ctx, projectID)
		if err != nil {
			writeError(c, http.StatusInternalServerError, "failed to load review-board plans")
			return
		}
		jobs, err := jobsRegistry.ListProject(ctx, projectID)
		if err != nil {
			writeError(c, http.StatusInternalServerError, "failed to load review-board workflows")
			return
		}
		settings := loadReviewBoardSettings(ctx, settingsRegistry, projectID)
		planByID := make(map[string]plans.Plan, len(projectPlans))
		for _, plan := range projectPlans {
			planByID[plan.ID] = plan
		}
		cards := make([]reviewBoardCard, 0, len(jobs))
		for _, job := range jobs {
			if !isReviewBoardRelevant(job) {
				continue
			}
			plan, exists := planByID[job.PlanID]
			if !exists {
				writeError(c, http.StatusConflict, "review workflow references an unavailable plan")
				return
			}
			card, buildErr := buildReviewBoardCard(ctx, plan, job, settings, executionsRegistry, checksRegistry, reviewsRegistry)
			if buildErr != nil {
				writeError(c, http.StatusInternalServerError, buildErr.Error())
				return
			}
			cards = append(cards, card)
		}
		sort.SliceStable(cards, func(left, right int) bool {
			return cards[left].UpdatedAt.After(cards[right].UpdatedAt)
		})
		writeData(c, http.StatusOK, reviewBoardResponse{Cards: cards})
	})

	api.GET("/jobs/:jobID/review-snapshot", func(c *gin.Context) {
		if !requireReviewBoardRegistries(c, plansRegistry, jobsRegistry, executionsRegistry, checksRegistry, reviewsRegistry) {
			return
		}
		ctx := c.Request.Context()
		job, err := jobsRegistry.Get(ctx, c.Param("jobID"))
		if err != nil {
			writeWorkflowJobError(c, err)
			return
		}
		plan, err := plansRegistry.Get(ctx, job.PlanID)
		if err != nil {
			writePlanError(c, err)
			return
		}
		settings := loadReviewBoardSettings(ctx, settingsRegistry, job.ProjectID)
		card, err := buildReviewBoardCard(ctx, plan, job, settings, executionsRegistry, checksRegistry, reviewsRegistry)
		if err != nil {
			writeError(c, http.StatusInternalServerError, err.Error())
			return
		}
		response := reviewSnapshotResponse{Card: card, CheckSteps: []checks.Step{}, Checkpoints: []execution.Checkpoint{}, Decisions: []approval.Decision{}}
		if card.Check != nil {
			response.CheckSteps, err = checksRegistry.ListSteps(ctx, card.Check.ID)
			if err != nil {
				writeError(c, http.StatusInternalServerError, "failed to load review check evidence")
				return
			}
		}
		if card.Execution != nil {
			response.Checkpoints, err = executionsRegistry.ListCheckpoints(ctx, card.Execution.ID)
			if err != nil {
				writeError(c, http.StatusInternalServerError, "failed to load review patch checkpoints")
				return
			}
		}
		if approvalsRegistry != nil {
			response.Decisions, err = approvalsRegistry.ListWorkflow(ctx, job.ID)
			if err != nil {
				writeError(c, http.StatusInternalServerError, "failed to load review decisions")
				return
			}
		}
		writeData(c, http.StatusOK, response)
	})
}

func requireReviewBoardRegistries(
	c *gin.Context,
	plansRegistry PlanRegistry,
	jobsRegistry WorkflowJobRegistry,
	executionsRegistry ExecutionRegistry,
	checksRegistry CheckRegistry,
	reviewsRegistry ReviewRegistry,
) bool {
	if plansRegistry == nil || jobsRegistry == nil || executionsRegistry == nil || checksRegistry == nil || reviewsRegistry == nil {
		writeError(c, http.StatusServiceUnavailable, "review board is unavailable")
		return false
	}
	return true
}

func loadReviewBoardSettings(ctx context.Context, registry AgentSettingsRegistry, projectID string) agentconfig.Settings {
	if registry == nil {
		return agentconfig.Settings{}
	}
	settings, err := registry.Get(ctx, projectID)
	if err != nil {
		return agentconfig.Settings{}
	}
	return settings
}

func buildReviewBoardCard(
	ctx context.Context,
	plan plans.Plan,
	job workflowjob.Job,
	settings agentconfig.Settings,
	executionsRegistry ExecutionRegistry,
	checksRegistry CheckRegistry,
	reviewsRegistry ReviewRegistry,
) (reviewBoardCard, error) {
	executions, err := executionsRegistry.ListWorkflow(ctx, job.ID)
	if err != nil {
		return reviewBoardCard{}, fmt.Errorf("failed to load review execution evidence")
	}
	checkRuns, err := checksRegistry.ListWorkflow(ctx, job.ID)
	if err != nil {
		return reviewBoardCard{}, fmt.Errorf("failed to load review check summary")
	}
	reviewRuns, err := reviewsRegistry.ListWorkflow(ctx, job.ID)
	if err != nil {
		return reviewBoardCard{}, fmt.Errorf("failed to load reviewer runs")
	}

	card := reviewBoardCard{
		Plan: plan, Workflow: job, Issues: []reviewer.Issue{}, PreviousIssues: []reviewer.Issue{},
		Executor: configuredReviewAgent(settings, agentconfig.RoleExecutor),
		Reviewer: configuredReviewAgent(settings, agentconfig.RoleReviewer),
		UpdatedAt: job.UpdatedAt,
	}
	if len(executions) > 0 {
		executionItem := executions[0]
		card.Execution = &executionItem
		card.Executor = reviewAgentAssignment{Provider: executionItem.Provider, Model: executionItem.Model, SettingsVersion: executionItem.AgentSettingsVersion}
		card.UpdatedAt = latestReviewTime(card.UpdatedAt, executionItem.UpdatedAt)
	}
	if len(checkRuns) > 0 {
		checkItem := checkRuns[0]
		card.Check = &checkItem
		card.UpdatedAt = latestReviewTime(card.UpdatedAt, checkItem.UpdatedAt)
	}
	if len(reviewRuns) > 0 {
		reviewItem := reviewRuns[0]
		card.Review = &reviewItem
		card.Reviewer = reviewAgentAssignment{Provider: reviewItem.Provider, Model: reviewItem.Model, SettingsVersion: reviewItem.AgentSettingsVersion}
		card.UpdatedAt = latestReviewTime(card.UpdatedAt, reviewItem.UpdatedAt)
		card.Issues, err = reviewsRegistry.ListIssues(ctx, reviewItem.ID)
		if err != nil {
			return reviewBoardCard{}, fmt.Errorf("failed to load current review findings")
		}
	}
	if len(reviewRuns) > 1 {
		previousItem := reviewRuns[1]
		card.PreviousReview = &previousItem
		card.PreviousIssues, err = reviewsRegistry.ListIssues(ctx, previousItem.ID)
		if err != nil {
			return reviewBoardCard{}, fmt.Errorf("failed to load previous review findings")
		}
	}
	card.Column = resolveReviewBoardColumn(job, card.Review)
	return card, nil
}

func configuredReviewAgent(settings agentconfig.Settings, role agentconfig.Role) reviewAgentAssignment {
	for _, agent := range settings.Agents {
		if agent.Role == role {
			return reviewAgentAssignment{Provider: defaultReviewValue(agent.Provider), Model: defaultReviewValue(agent.Model), SettingsVersion: settings.Version}
		}
	}
	return reviewAgentAssignment{Provider: "daemon default", Model: "daemon default"}
}

func defaultReviewValue(value string) string {
	if value == "" {
		return "daemon default"
	}
	return value
}

func isReviewBoardRelevant(job workflowjob.Job) bool {
	status := string(job.Status)
	if status == "REJECTED" || status == "CANCELLED" || status == "READY" || status == "WORKSPACE_PREPARING" {
		return false
	}
	return job.ExecutionVersion > 0 || status == "CHECKING" || status == "REVIEWING" || status == "WAITING_FOR_APPROVAL" || status == "APPROVED" || status == "PUBLISHING" || status == "COMPLETED" || status == "FAILED"
}

func resolveReviewBoardColumn(job workflowjob.Job, reviewRun *reviewer.Run) string {
	status := string(job.Status)
	if status == "APPROVED" || status == "PUBLISHING" || status == "COMPLETED" {
		return "APPROVED"
	}
	if status == "WAITING_FOR_APPROVAL" {
		if reviewRun != nil && (reviewRun.Verdict == reviewer.VerdictRequestRevision || reviewRun.BlockingIssues > 0) {
			return "ISSUES_FOUND"
		}
		return "READY_FOR_APPROVAL"
	}
	if status == "REVISION_REQUIRED" {
		return "REVISION_IN_PROGRESS"
	}
	if status == "QUEUED" || status == "EXECUTING" || status == "CHECKING" {
		if job.RevisionCount > 0 || job.ExecutionVersion > 1 {
			return "REVISION_IN_PROGRESS"
		}
		return "AWAITING_REVIEW"
	}
	if status == "REVIEWING" {
		if reviewRun != nil && reviewRun.Status == reviewer.StatusRunning {
			return "AI_REVIEWING"
		}
		if job.RevisionCount > 0 || job.ExecutionVersion > 1 {
			return "RE_REVIEW"
		}
		return "AWAITING_REVIEW"
	}
	if status == "FAILED" {
		return "ISSUES_FOUND"
	}
	return "AWAITING_REVIEW"
}

func latestReviewTime(current, candidate time.Time) time.Time {
	if candidate.After(current) {
		return candidate
	}
	return current
}
