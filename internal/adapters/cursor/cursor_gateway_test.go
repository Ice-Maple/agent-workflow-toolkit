package cursor_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"agent-workflow-toolkit/internal/adapters/cursor"
	"agent-workflow-toolkit/internal/gateway"
	"agent-workflow-toolkit/internal/orchestration"
	"agent-workflow-toolkit/internal/policy"
	"agent-workflow-toolkit/internal/storage"
)

func TestCursorAdapterMapsLaunchObserveAndCompletionIntoCoreCheckpoint(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := storage.New(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	binding := orchestration.Binding{Role: "reviewer", Adapter: cursor.Provider, Model: "fixture-model", Reasoning: "none", Permission: "read-only", Workspace: workspace}
	config := policy.DefaultConfig()
	config.RegisteredBindings = []orchestration.Binding{binding}
	core, err := gateway.New(store, config)
	if err != nil {
		t.Fatal(err)
	}
	task, err := core.CreateTask(gateway.CreateTaskInput{Title: "Cursor W3", Objective: "review fixture", Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	node, err := core.ProposeNode(gateway.ProposeNodeInput{TaskID: task.Task.ID, Kind: "review", Role: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	preflight, err := core.RunPreflight(gateway.RunPreflightInput{NodeID: node.Node.ID, Binding: binding})
	if err != nil || preflight.Preflight.Status != "passed" {
		t.Fatalf("core preflight = %#v, err=%v", preflight, err)
	}
	launch, err := core.PrepareAttempt(gateway.PrepareAttemptInput{NodeID: node.Node.ID, ControllerInstanceID: "cursor-test-controller"})
	if err != nil {
		t.Fatal(err)
	}

	adapter := cursor.New(os.Args[0])
	adapter.Environment = []string{"CURSOR_ADAPTER_HELPER=1"}
	adapter.StartupTimeout = 2 * time.Second
	running, err := adapter.Launch(t.Context(), cursor.LaunchRequest{Workspace: workspace, Model: binding.Model, Mode: "plan", Permission: binding.Permission, TrustAuthorized: true, Prompt: "return the smoke token", TaskCanary: launch.Attempt.TaskCanary})
	if err != nil {
		t.Fatalf("adapter launch = %v", err)
	}
	launchReceipt, err := core.SubmitHostActionResult(gateway.SubmitHostActionResultInput{AttemptID: launch.Attempt.ID, ActionID: launch.LaunchAction.ID, Lease: launch.LaunchAction.Lease, Result: running.HostActionResult()})
	if err != nil {
		t.Fatalf("submit launch = %v", err)
	}
	if len(launchReceipt.Next) != 1 || launchReceipt.Next[0].Kind != orchestration.ActionObserve {
		t.Fatalf("next actions after launch = %#v", launchReceipt.Next)
	}

	observeAction := launchReceipt.Next[0]
	terminal := waitCursorTerminal(t, adapter, running.ProviderHandle)
	observeReceipt, err := core.SubmitHostActionResult(gateway.SubmitHostActionResultInput{AttemptID: launch.Attempt.ID, ActionID: observeAction.ID, Lease: observeAction.Lease, Result: terminal.HostActionResult()})
	if err != nil {
		t.Fatalf("submit observe = %v", err)
	}
	if observeReceipt.Attempt.Status != orchestration.AttemptCompleted || !terminal.Terminal {
		t.Fatalf("completed attempt = %#v, adapter=%#v", observeReceipt.Attempt, terminal)
	}
	completion, err := core.RequestCompletion(gateway.CompletionInput{AttemptID: launch.Attempt.ID})
	if err != nil {
		t.Fatal(err)
	}
	if completion.Decision.Status != orchestration.AttemptCompleted || completion.Decision.Reason != "terminal_event_identity_and_output_validated" {
		t.Fatalf("completion = %#v", completion.Decision)
	}
	if completion.Node.Status != orchestration.NodeAwaitingHumanDecision {
		t.Fatalf("node did not stop at human checkpoint: %#v", completion.Node)
	}
}

func waitCursorTerminal(t *testing.T, adapter *cursor.Adapter, handle string) cursor.ExecutionResult {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		result, err := adapter.Observe(t.Context(), cursor.ObserveRequest{HandleID: handle})
		if err != nil {
			t.Fatal(err)
		}
		if result.Terminal {
			return result
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for Cursor terminal result")
	return cursor.ExecutionResult{}
}
