// Package native defines the host-owned action boundary for Codex App Native
// subagents. The Go core signs actions; a Controller Skill supplies the real
// native host capability. FakeExecutor exists only for deterministic contract
// tests and never launches a CLI process.
package native

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"agent-workflow-toolkit/internal/orchestration"
)

// Executor is the narrow contract between a Controller and a Native host.
// Issue records the exact action returned by the core. Execute may only run an
// action that was issued first, and must be idempotent by action ID.
type Executor interface {
	Issue(orchestration.HostAction) error
	Execute(context.Context, string) (orchestration.HostActionResult, error)
}

type Call struct {
	ActionID  string
	AttemptID string
	Kind      orchestration.ActionKind
}

// FakeExecutor is a no-side-effect Native contract fixture. It models the
// action/receipt boundary and returns a provider identity derived from the
// action binding, but it does not invoke Codex, a shell, or an external CLI.
type FakeExecutor struct {
	provider  string
	mu        sync.Mutex
	issued    map[string]orchestration.HostAction
	completed map[string]orchestration.HostActionResult
	sequence  map[string]int
	calls     []Call
}

func NewFakeExecutor(provider string) *FakeExecutor {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = "codex-native"
	}
	return &FakeExecutor{
		provider:  provider,
		issued:    map[string]orchestration.HostAction{},
		completed: map[string]orchestration.HostActionResult{},
		sequence:  map[string]int{},
	}
}

func (e *FakeExecutor) Issue(action orchestration.HostAction) error {
	if err := validateAction(action); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if previous, ok := e.issued[action.ID]; ok {
		if !sameIssuedAction(previous, action) {
			return fmt.Errorf("action id %q was re-used with different content", action.ID)
		}
		return nil
	}
	if action.Sequence <= e.sequence[action.AttemptID] {
		return fmt.Errorf("action sequence must increase for attempt %q", action.AttemptID)
	}
	e.issued[action.ID] = action
	e.sequence[action.AttemptID] = action.Sequence
	return nil
}

func (e *FakeExecutor) Execute(ctx context.Context, actionID string) (orchestration.HostActionResult, error) {
	if ctx == nil {
		return orchestration.HostActionResult{}, errors.New("context is required")
	}
	e.mu.Lock()
	if result, ok := e.completed[actionID]; ok {
		e.mu.Unlock()
		return result, nil
	}
	action, ok := e.issued[actionID]
	e.mu.Unlock()
	if !ok {
		return orchestration.HostActionResult{}, fmt.Errorf("native action %q was not issued by core", actionID)
	}
	select {
	case <-ctx.Done():
		return orchestration.HostActionResult{}, ctx.Err()
	default:
	}
	result, err := e.resultFor(action)
	if err != nil {
		return orchestration.HostActionResult{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if previous, ok := e.completed[actionID]; ok {
		return previous, nil
	}
	e.completed[actionID] = result
	e.calls = append(e.calls, Call{ActionID: action.ID, AttemptID: action.AttemptID, Kind: action.Kind})
	return result, nil
}

func (e *FakeExecutor) Calls() []Call {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := make([]Call, len(e.calls))
	copy(result, e.calls)
	return result
}

func (e *FakeExecutor) resultFor(action orchestration.HostAction) (orchestration.HostActionResult, error) {
	provider, workspace := bindingIdentity(action.Parameters, e.provider)
	session := "native-session-" + action.AttemptID
	identity := map[string]string{"provider": provider, "session": session}
	if workspace != "" {
		identity["workspace"] = workspace
	}
	canary, _ := action.Parameters["task_canary"].(string)
	result := orchestration.HostActionResult{
		ProviderSessionID: session,
		SideEffectState:   "none",
		Identity:          identity,
		TaskCanary:        canary,
	}
	switch action.Kind {
	case orchestration.ActionLaunch:
		started := true
		result.Status = "running"
		result.ProviderHandle = "native-handle-" + action.AttemptID
		result.ExecutionStarted = &started
	case orchestration.ActionObserve:
		started := true
		result.Status = "completed"
		result.Terminal = true
		result.ProviderHandle = "native-handle-" + action.AttemptID
		result.ExecutionStarted = &started
	case orchestration.ActionSend:
		result.Status = "delivered"
	case orchestration.ActionCancelHost:
		result.Status = "cancelled"
		result.Terminal = true
	case orchestration.ActionCleanup:
		result.Status = "cleaned"
		result.Terminal = true
	default:
		return orchestration.HostActionResult{}, fmt.Errorf("unsupported native action kind %q", action.Kind)
	}
	return result, nil
}

func validateAction(action orchestration.HostAction) error {
	if strings.TrimSpace(action.ID) == "" || strings.TrimSpace(action.AttemptID) == "" || strings.TrimSpace(action.Lease) == "" {
		return errors.New("native action id, attempt_id and lease are required")
	}
	if action.Sequence < 1 {
		return errors.New("native action sequence must be positive")
	}
	if action.Status != orchestration.ActionPending {
		return fmt.Errorf("native action %q is not pending", action.ID)
	}
	if !knownActionKind(action.Kind) {
		return fmt.Errorf("unsupported native action kind %q", action.Kind)
	}
	if strings.TrimSpace(action.ParametersHash) == "" || action.ParametersHash != orchestration.HashParameters(action.Parameters) {
		return fmt.Errorf("native action %q has an invalid parameters hash", action.ID)
	}
	return nil
}

func sameIssuedAction(left, right orchestration.HostAction) bool {
	return left.AttemptID == right.AttemptID && left.Kind == right.Kind && left.Sequence == right.Sequence &&
		left.Lease == right.Lease && left.ParametersHash == right.ParametersHash
}

func knownActionKind(kind orchestration.ActionKind) bool {
	switch kind {
	case orchestration.ActionLaunch, orchestration.ActionObserve, orchestration.ActionSend, orchestration.ActionCancelHost, orchestration.ActionCleanup:
		return true
	default:
		return false
	}
}

func bindingIdentity(parameters map[string]any, defaultProvider string) (provider, workspace string) {
	provider = defaultProvider
	if binding, ok := parameters["binding"].(orchestration.Binding); ok {
		if strings.TrimSpace(binding.Adapter) != "" {
			provider = binding.Adapter
		}
		workspace = binding.Workspace
	} else if binding, ok := parameters["binding"].(map[string]any); ok {
		if value, ok := binding["adapter"].(string); ok && strings.TrimSpace(value) != "" {
			provider = value
		}
		if value, ok := binding["workspace"].(string); ok {
			workspace = value
		}
	}
	if value, ok := parameters["workspace"].(string); ok && strings.TrimSpace(value) != "" {
		workspace = value
	}
	return provider, workspace
}
