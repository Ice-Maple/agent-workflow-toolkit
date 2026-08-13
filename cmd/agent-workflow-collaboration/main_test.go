package main

import (
	"testing"

	"agent-workflow-toolkit/internal/orchestration"
)

func TestRequestedInstancesUsesDefaultCount(t *testing.T) {
	plan := orchestration.CollaborationPlan{Steps: []orchestration.CollaborationStepSpec{{ID: "one"}, {ID: "two", Count: 2}}}
	if got := requestedInstances(plan); got != 3 {
		t.Fatalf("requestedInstances() = %d", got)
	}
}
