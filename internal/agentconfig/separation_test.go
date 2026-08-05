package agentconfig

import (
	"errors"
	"testing"
)

func TestValidateAggregateRejectsSameExecutorAndReviewer(t *testing.T) {
	agents := defaultAgents()
	agents[1].Provider, agents[1].Model = "shared", "same-model"
	agents[2].Provider, agents[2].Model = "shared", "same-model"
	if err := validateAggregate(agents, defaultPolicies()); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("expected invalid settings, got %v", err)
	}
}

func TestValidateAggregateAllowsDifferentReviewModel(t *testing.T) {
	agents := defaultAgents()
	agents[1].Provider, agents[1].Model = "shared", "executor-model"
	agents[2].Provider, agents[2].Model = "shared", "review-model"
	if err := validateAggregate(agents, defaultPolicies()); err != nil {
		t.Fatal(err)
	}
}
