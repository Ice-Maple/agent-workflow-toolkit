package fake

import (
	"reflect"
	"testing"
)

func TestPreflightRequiresExplicitBinding(t *testing.T) {
	adapter := NewAdapter()
	if got := adapter.Preflight(Binding{Role: "reviewer", Adapter: "fake", Model: "fixture-model"}); got.Status != "blocked" {
		t.Fatalf("incomplete binding status = %q, want blocked", got.Status)
	}
	if got := adapter.Preflight(Binding{Role: "reviewer", Adapter: "fake", Model: "fixture-model", Reasoning: "high"}); got.Status != "passed" {
		t.Fatalf("complete binding status = %q, want passed", got.Status)
	}
}

func TestBridgeRejectsUnissuedActionAndDeduplicatesIssuedAction(t *testing.T) {
	adapter := NewAdapter()
	bridge := NewBridge(adapter)
	if _, err := bridge.Execute("not-issued"); err == nil {
		t.Fatal("Execute() accepted an action that core never issued")
	}
	action := HostAction{ID: "action-1", AttemptID: "attempt-1", Kind: Launch, Sequence: 1, Lease: "lease-1", Parameters: map[string]any{"model": "fixture-model"}}
	if err := bridge.Issue(action); err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	first, err := bridge.Execute(action.ID)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	second, err := bridge.Execute(action.ID)
	if err != nil {
		t.Fatalf("duplicate Execute() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("duplicate result changed: first=%#v second=%#v", first, second)
	}
	if calls := adapter.Calls(); len(calls) != 1 {
		t.Fatalf("adapter calls = %d, want 1 after duplicate action", len(calls))
	}
}

func TestBridgeRejectsActionSequenceRegression(t *testing.T) {
	bridge := NewBridge(NewAdapter())
	first := HostAction{ID: "action-1", AttemptID: "attempt-1", Kind: Launch, Sequence: 2, Lease: "lease-1"}
	if err := bridge.Issue(first); err != nil {
		t.Fatalf("Issue(first) error = %v", err)
	}
	regression := HostAction{ID: "action-2", AttemptID: "attempt-1", Kind: Observe, Sequence: 1, Lease: "lease-2"}
	if err := bridge.Issue(regression); err == nil {
		t.Fatal("Issue() accepted a sequence regression")
	}
}
