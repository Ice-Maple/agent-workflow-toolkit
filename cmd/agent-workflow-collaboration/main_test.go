package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
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
	manifest, err := loadCollaborationInput("", "sample/reviewer", "review the current diff", "", "")
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
		if _, err := loadCollaborationInput(input.manifest, input.role, input.task, "", ""); err == nil {
			t.Fatalf("input unexpectedly succeeded: %#v", input)
		}
	}
}

func TestLoadCollaborationInputFreezesDedicatedReadOnlyArtifact(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "git-review.md")
	content := []byte("# Frozen Git review input\n\n## Git status\n\n```text\n M file.go\n```\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	manifest, err := loadCollaborationInput("", "sample/reviewer", "review frozen input", path, hash)
	if err != nil {
		t.Fatal(err)
	}
	inputs := manifest.Steps[0].FrozenInputs
	if len(inputs) != 1 || inputs[0].Path != path || inputs[0].Root != root || inputs[0].SHA256 != hash || inputs[0].Bytes != int64(len(content)) {
		t.Fatalf("frozen inputs = %#v", inputs)
	}
	if _, err := loadCollaborationInput("", "sample/reviewer", "review frozen input", path, strings.Repeat("0", 64)); err == nil {
		t.Fatal("mismatched frozen input hash unexpectedly passed")
	}
}
