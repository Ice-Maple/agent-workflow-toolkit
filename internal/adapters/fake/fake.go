// Package fake provides a deterministic, no-side-effect Adapter used by
// Wave 0 contract tests. It is not a production Provider Adapter.
package fake

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

type ActionKind string

const (
	Launch     ActionKind = "launch"
	Observe    ActionKind = "observe"
	Send       ActionKind = "send"
	CancelHost ActionKind = "cancel_host"
	Cleanup    ActionKind = "cleanup"
)

type Binding struct {
	Role      string
	Adapter   string
	Model     string
	Reasoning string
}

type PreflightDecision struct {
	Status string
	Code   string
}

type HostAction struct {
	ID             string
	AttemptID      string
	Kind           ActionKind
	Sequence       int
	Lease          string
	Parameters     map[string]any
	ParametersHash string
}

type Result struct {
	ActionID       string
	AttemptID      string
	Kind           ActionKind
	Status         string
	ProviderHandle string
	Terminal       bool
	SideEffect     string
	Identity       map[string]string
	TaskCanary     string
}

type Call struct {
	ActionID  string
	AttemptID string
	Kind      ActionKind
}

type Adapter struct {
	mu    sync.Mutex
	calls []Call
}

func NewAdapter() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Capabilities() map[ActionKind]string {
	return map[ActionKind]string{
		Launch:     "supported",
		Observe:    "supported",
		Send:       "supported",
		CancelHost: "supported",
		Cleanup:    "supported",
	}
}

func (a *Adapter) Preflight(binding Binding) PreflightDecision {
	if binding.Role == "" || binding.Adapter == "" || binding.Model == "" || binding.Reasoning == "" {
		return PreflightDecision{Status: "blocked", Code: "binding_incomplete"}
	}
	if binding.Adapter != "fake" {
		return PreflightDecision{Status: "blocked", Code: "adapter_not_registered"}
	}
	return PreflightDecision{Status: "passed", Code: ""}
}

func (a *Adapter) Execute(action HostAction) (Result, error) {
	if action.ID == "" || action.AttemptID == "" || action.Lease == "" || action.Sequence < 1 {
		return Result{}, fmt.Errorf("invalid host action metadata")
	}
	if action.ParametersHash == "" {
		action.ParametersHash = ParametersHash(action.Parameters)
	}
	a.mu.Lock()
	a.calls = append(a.calls, Call{ActionID: action.ID, AttemptID: action.AttemptID, Kind: action.Kind})
	a.mu.Unlock()

	identity := map[string]string{
		"provider": "fake",
		"session":  "fake-session-" + action.AttemptID,
	}
	if workspace, ok := action.Parameters["workspace"].(string); ok && workspace != "" {
		identity["workspace"] = workspace
	}
	result := Result{ActionID: action.ID, AttemptID: action.AttemptID, Kind: action.Kind, SideEffect: "none", Identity: identity}
	if canary, ok := action.Parameters["task_canary"].(string); ok {
		result.TaskCanary = canary
	}
	switch action.Kind {
	case Launch:
		result.Status = "running"
		result.ProviderHandle = "fake-handle-" + action.AttemptID
	case Observe:
		result.Status = "completed"
		result.Terminal = true
	case Send:
		result.Status = "delivered"
	case CancelHost:
		result.Status = "cancelled"
		result.Terminal = true
	case Cleanup:
		result.Status = "cleaned"
	default:
		return Result{}, fmt.Errorf("unsupported fake action kind %q", action.Kind)
	}
	return result, nil
}

func (a *Adapter) Calls() []Call {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]Call, len(a.calls))
	copy(result, a.calls)
	return result
}

type Bridge struct {
	adapter   *Adapter
	mu        sync.Mutex
	issued    map[string]HostAction
	completed map[string]Result
	sequence  map[string]int
}

func NewBridge(adapter *Adapter) *Bridge {
	return &Bridge{adapter: adapter, issued: map[string]HostAction{}, completed: map[string]Result{}, sequence: map[string]int{}}
}

func (b *Bridge) Issue(action HostAction) error {
	if action.ID == "" || action.AttemptID == "" || action.Lease == "" || action.Sequence < 1 {
		return fmt.Errorf("invalid host action metadata")
	}
	if action.ParametersHash == "" {
		action.ParametersHash = ParametersHash(action.Parameters)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if previous, ok := b.issued[action.ID]; ok {
		if previous.AttemptID != action.AttemptID || previous.Kind != action.Kind || previous.ParametersHash != action.ParametersHash {
			return fmt.Errorf("action id %q was re-used with different content", action.ID)
		}
		return nil
	}
	if action.Sequence <= b.sequence[action.AttemptID] {
		return fmt.Errorf("action sequence must increase for attempt %q", action.AttemptID)
	}
	b.issued[action.ID] = action
	b.sequence[action.AttemptID] = action.Sequence
	return nil
}

func (b *Bridge) Execute(actionID string) (Result, error) {
	b.mu.Lock()
	if result, ok := b.completed[actionID]; ok {
		b.mu.Unlock()
		return result, nil
	}
	action, ok := b.issued[actionID]
	b.mu.Unlock()
	if !ok {
		return Result{}, fmt.Errorf("host action %q was not issued by core", actionID)
	}
	result, err := b.adapter.Execute(action)
	if err != nil {
		return Result{}, err
	}
	b.mu.Lock()
	b.completed[actionID] = result
	b.mu.Unlock()
	return result, nil
}

func ParametersHash(parameters map[string]any) string {
	if parameters == nil {
		parameters = map[string]any{}
	}
	// encoding/json sorts string map keys, giving the fixture a stable hash
	// without adding a canonicalization dependency in Wave 0.
	raw, _ := json.Marshal(parameters)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func SupportedActionKinds() []ActionKind {
	result := []ActionKind{Launch, Observe, Send, CancelHost, Cleanup}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
