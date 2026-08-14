package main

import (
	"path/filepath"
	"testing"

	"agent-workflow-toolkit/internal/orchestration"
)

func TestRequestedInstancesUsesDefaultCount(t *testing.T) {
	plan := orchestration.CollaborationPlan{Steps: []orchestration.CollaborationStepSpec{{ID: "one"}, {ID: "two", Count: 2}}}
	if got := requestedInstances(plan); got != 3 {
		t.Fatalf("requestedInstances() = %d", got)
	}
}

func TestLoadCollaborationInputBuildsOneStepProjectRolePlan(t *testing.T) {
	manifest, err := loadCollaborationInput("", "sample/reviewer", "review the current diff")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != "collaboration/v1" || len(manifest.RoleDependencies) != 1 || manifest.RoleDependencies[0] != "sample/reviewer" || len(manifest.Steps) != 1 {
		t.Fatalf("project Role manifest = %#v", manifest)
	}
	step := manifest.Steps[0]
	if step.ID != "project-role" || step.Role != "sample/reviewer" || step.Task != "review the current diff" || step.Count != 1 {
		t.Fatalf("project Role step = %#v", step)
	}
}

func TestLoadCollaborationInputRequiresOneCompleteInputForm(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "collaboration.yaml")
	for _, input := range []struct {
		manifest string
		role     string
		task     string
	}{
		{},
		{role: "sample/reviewer"},
		{task: "review"},
		{manifest: manifestPath, role: "sample/reviewer", task: "review"},
	} {
		if _, err := loadCollaborationInput(input.manifest, input.role, input.task); err == nil {
			t.Fatalf("input unexpectedly succeeded: %#v", input)
		}
	}
}
