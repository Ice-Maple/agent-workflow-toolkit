package native

import (
	"context"
	"reflect"
	"testing"

	"agent-workflow-toolkit/internal/orchestration"
)

func TestFakeNativeExecutorRequiresCoreIssuedActionsAndIsIdempotent(t *testing.T) {
	executor := NewFakeExecutor("")
	action := nativeAction(orchestration.ActionLaunch, 1)
	if _, err := executor.Execute(context.Background(), action.ID); err == nil {
		t.Fatal("unissued native action was executed")
	}
	if len(executor.Calls()) != 0 {
		t.Fatal("unissued native action produced a host call")
	}
	if err := executor.Issue(action); err != nil {
		t.Fatal(err)
	}
	first, err := executor.Execute(context.Background(), action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "running" || first.ProviderHandle == "" || first.Identity["provider"] != "codex-native" || first.TaskCanary != "canary-1" {
		t.Fatalf("native launch result = %#v", first)
	}
	second, err := executor.Execute(context.Background(), action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || len(executor.Calls()) != 1 {
		t.Fatalf("native action replay was not idempotent: first=%#v second=%#v calls=%#v", first, second, executor.Calls())
	}
	tampered := action
	tampered.Lease = "different-lease"
	if err := executor.Issue(tampered); err == nil {
		t.Fatal("same action id accepted changed lease")
	}
}

func TestFakeNativeExecutorPreservesActionOrderAndMapsNativeResults(t *testing.T) {
	executor := NewFakeExecutor("")
	kinds := []orchestration.ActionKind{orchestration.ActionLaunch, orchestration.ActionObserve, orchestration.ActionSend, orchestration.ActionCancelHost, orchestration.ActionCleanup}
	for index, kind := range kinds {
		action := nativeAction(kind, index+1)
		if err := executor.Issue(action); err != nil {
			t.Fatal(err)
		}
		result, err := executor.Execute(context.Background(), action.ID)
		if err != nil {
			t.Fatal(err)
		}
		if result.Identity["provider"] != "codex-native" || result.Identity["workspace"] == "" || result.TaskCanary != "canary-1" {
			t.Fatalf("native %s identity = %#v", kind, result)
		}
		if kind == orchestration.ActionObserve && (!result.Terminal || result.Status != "completed") {
			t.Fatalf("native observe result = %#v", result)
		}
	}
	calls := executor.Calls()
	if len(calls) != len(kinds) {
		t.Fatalf("native calls = %#v", calls)
	}
	for index, call := range calls {
		if call.Kind != kinds[index] || call.AttemptID != "attempt-1" {
			t.Fatalf("native call[%d] = %#v", index, call)
		}
	}
}

func TestFakeNativeExecutorHonorsCancellationBeforeHostCall(t *testing.T) {
	executor := NewFakeExecutor("")
	action := nativeAction(orchestration.ActionSend, 1)
	if err := executor.Issue(action); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := executor.Execute(ctx, action.ID); err == nil {
		t.Fatal("cancelled native action was executed")
	}
	if len(executor.Calls()) != 0 {
		t.Fatal("cancelled native action produced a host call")
	}
}

func nativeAction(kind orchestration.ActionKind, sequence int) orchestration.HostAction {
	parameters := map[string]any{
		"task_canary": "canary-1",
		"workspace":   `E:\workspace`,
		"binding": orchestration.Binding{
			Adapter:   "codex-native",
			Workspace: `E:\workspace`,
		},
	}
	return orchestration.HostAction{
		ID:             "action-" + string(rune('0'+sequence)),
		AttemptID:      "attempt-1",
		Kind:           kind,
		Sequence:       sequence,
		Lease:          "lease-" + string(rune('0'+sequence)),
		Parameters:     parameters,
		ParametersHash: orchestration.HashParameters(parameters),
		Status:         orchestration.ActionPending,
	}
}
