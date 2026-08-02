package workflowjob

import "fmt"

type Status string

type Action string

const (
	StatusReady              Status = "READY"
	StatusWorkspacePreparing Status = "WORKSPACE_PREPARING"
	StatusQueued             Status = "QUEUED"
	StatusExecuting          Status = "EXECUTING"
	StatusChecking           Status = "CHECKING"
	StatusReviewing          Status = "REVIEWING"
	StatusWaitingApproval    Status = "WAITING_FOR_APPROVAL"
	StatusRevisionRequired   Status = "REVISION_REQUIRED"
	StatusApproved           Status = "APPROVED"
	StatusPublishing         Status = "PUBLISHING"
	StatusCompleted          Status = "COMPLETED"
	StatusFailed             Status = "FAILED"
	StatusRejected           Status = "REJECTED"
	StatusCancelled          Status = "CANCELLED"
)

const (
	ActionCreate               Action = "CREATE"
	ActionStart                Action = "START"
	ActionWorkspaceReady       Action = "WORKSPACE_READY"
	ActionExecutionStarted     Action = "EXECUTION_STARTED"
	ActionExecutionCompleted   Action = "EXECUTION_COMPLETED"
	ActionChecksCompleted      Action = "CHECKS_COMPLETED"
	ActionReviewCompleted      Action = "REVIEW_COMPLETED"
	ActionApprove              Action = "APPROVE"
	ActionRequestRevision      Action = "REQUEST_REVISION"
	ActionQueueRevision        Action = "QUEUE_REVISION"
	ActionReject               Action = "REJECT"
	ActionPublish              Action = "PUBLISH"
	ActionPublicationCompleted Action = "PUBLICATION_COMPLETED"
	ActionFail                 Action = "FAIL"
	ActionRetry                Action = "RETRY"
	ActionCancel               Action = "CANCEL"
)

var transitionTable = map[Status]map[Action]Status{
	StatusReady: {
		ActionStart: StatusWorkspacePreparing,
	},
	StatusWorkspacePreparing: {
		ActionWorkspaceReady: StatusQueued,
	},
	StatusQueued: {
		ActionExecutionStarted: StatusExecuting,
	},
	StatusExecuting: {
		ActionExecutionCompleted: StatusChecking,
	},
	StatusChecking: {
		ActionChecksCompleted: StatusReviewing,
	},
	StatusReviewing: {
		ActionReviewCompleted: StatusWaitingApproval,
	},
	StatusWaitingApproval: {
		ActionApprove:         StatusApproved,
		ActionRequestRevision: StatusRevisionRequired,
		ActionReject:          StatusRejected,
	},
	StatusRevisionRequired: {
		ActionQueueRevision: StatusQueued,
	},
	StatusApproved: {
		ActionPublish: StatusPublishing,
	},
	StatusPublishing: {
		ActionPublicationCompleted: StatusCompleted,
	},
}

var failureStatuses = map[Status]struct{}{
	StatusWorkspacePreparing: {},
	StatusQueued:             {},
	StatusExecuting:          {},
	StatusChecking:           {},
	StatusReviewing:          {},
	StatusPublishing:         {},
}

func nextStatus(current Status, action Action, retryStatus Status) (Status, error) {
	if action == ActionCancel {
		if isTerminal(current) {
			return "", invalidTransition(current, action)
		}
		return StatusCancelled, nil
	}
	if action == ActionFail {
		if _, allowed := failureStatuses[current]; !allowed {
			return "", invalidTransition(current, action)
		}
		return StatusFailed, nil
	}
	if action == ActionRetry {
		if current != StatusFailed || retryStatus == "" {
			return "", invalidTransition(current, action)
		}
		if _, retryable := failureStatuses[retryStatus]; !retryable {
			return "", invalidTransition(current, action)
		}
		return retryStatus, nil
	}
	if transitions, exists := transitionTable[current]; exists {
		if next, allowed := transitions[action]; allowed {
			return next, nil
		}
	}
	return "", invalidTransition(current, action)
}

func dispatchFor(action Action) (string, bool) {
	switch action {
	case ActionStart:
		return "workflow.prepare_workspace", true
	case ActionWorkspaceReady, ActionQueueRevision:
		return "workflow.execute", true
	case ActionExecutionCompleted:
		return "workflow.run_checks", true
	case ActionChecksCompleted:
		return "workflow.review", true
	case ActionPublish:
		return "workflow.publish", true
	default:
		return "", false
	}
}

func isTerminal(status Status) bool {
	switch status {
	case StatusCompleted, StatusRejected, StatusCancelled:
		return true
	default:
		return false
	}
}

func invalidTransition(status Status, action Action) error {
	return fmt.Errorf("%w: action %s is not allowed from %s", ErrInvalidTransition, action, status)
}
