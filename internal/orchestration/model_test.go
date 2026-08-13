package orchestration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHashParametersIsStableAcrossJSONTransport(t *testing.T) {
	parameters := map[string]any{"binding": Binding{Role: "reviewer", Adapter: "cursor", Model: "fixture", Permission: "read-only"}, "expected_output": ExpectedOutput{Required: false}}
	want := HashParameters(parameters)
	raw, err := json.Marshal(parameters)
	if err != nil {
		t.Fatal(err)
	}
	var transported map[string]any
	if err := json.Unmarshal(raw, &transported); err != nil {
		t.Fatal(err)
	}
	if got := HashParameters(transported); got != want {
		t.Fatalf("transported hash = %s, want %s", got, want)
	}
}

func TestValidateAttemptTransitionCoversTerminalAndCancelBoundaries(t *testing.T) {
	allowed := [][2]AttemptStatus{
		{AttemptPrepared, AttemptLaunching},
		{AttemptLaunching, AttemptRunning},
		{AttemptRunning, AttemptCompleted},
		{AttemptRunning, AttemptTimedOut},
		{AttemptRunning, AttemptCancelRequested},
		{AttemptCancelRequested, AttemptCancelled},
		{AttemptCancelRequested, AttemptCancelUnconfirmed},
		{AttemptCancelUnconfirmed, AttemptIncomplete},
	}
	for _, pair := range allowed {
		if err := ValidateAttemptTransition(pair[0], pair[1]); err != nil {
			t.Errorf("allowed transition %q -> %q failed: %v", pair[0], pair[1], err)
		}
	}
	if err := ValidateAttemptTransition(AttemptCompleted, AttemptRunning); err == nil {
		t.Fatal("terminal attempt was allowed to move back to running")
	}
	if err := ValidateAttemptTransition(AttemptRunning, AttemptLaunching); err == nil {
		t.Fatal("running attempt was allowed to move back to launching")
	}
}

func TestNewTaskRequiresExistingDirectoryAndStoresCanonicalWorkspace(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	task, err := NewTask("title", "objective", workspace+string(filepath.Separator))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := CanonicalDirectory(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if task.Workspace != canonical {
		t.Fatalf("task workspace = %q, want canonical %q", task.Workspace, canonical)
	}
	if _, err := NewTask("title", "objective", filepath.Join(root, "missing")); err == nil {
		t.Fatal("task accepted a missing workspace")
	}
}

func TestRevisionConstructionPreservesParentLineage(t *testing.T) {
	root := t.TempDir()
	task, err := NewTask("title", "objective", root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := NewRevision(task.ID, task.CurrentRevisionID, "first revision")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewRevision(task.ID, first.ID, "second revision")
	if err != nil {
		t.Fatal(err)
	}
	if second.ParentRevisionID != first.ID || first.Instruction != "first revision" {
		t.Fatalf("revision lineage was not preserved: first=%#v second=%#v", first, second)
	}
}
