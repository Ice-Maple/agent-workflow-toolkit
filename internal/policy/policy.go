// Package policy contains deterministic admission and retry decisions. It
// does not know how a provider is launched; it only decides whether a launch
// is allowed and whether a recorded failure is safe to retry.
package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"agent-workflow-toolkit/internal/orchestration"
)

type Config struct {
	RegisteredBindings []orchestration.Binding `json:"registered_bindings"`
	RetryMode          string                  `json:"retry_mode"`
	MaxAutoRetries     int                     `json:"max_auto_retries"`
	FallbackMode       string                  `json:"fallback_mode"`
	MCPIsolation       string                  `json:"mcp_isolation"`
	GoalEnabled        bool                    `json:"goal_enabled"`
}

type PreflightDecision struct {
	Status       string                `json:"status"`
	Code         string                `json:"code,omitempty"`
	Reason       string                `json:"reason,omitempty"`
	MCPIsolation string                `json:"mcp_isolation,omitempty"`
	Binding      orchestration.Binding `json:"binding"`
}

type FallbackDecision struct {
	Status string `json:"status"`
	Code   string `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func DefaultConfig() Config {
	return Config{RetryMode: "limited_auto", MaxAutoRetries: 1, FallbackMode: "ask", MCPIsolation: "strict", GoalEnabled: false}
}

// WithDefaults materializes the fail-closed policy defaults before validation
// or execution. Keeping this separate from Validate avoids a value-receiver
// validation call silently discarding normalized values.
func WithDefaults(config Config) Config {
	if config.RetryMode == "" {
		config.RetryMode = "limited_auto"
		if config.MaxAutoRetries == 0 {
			config.MaxAutoRetries = 1
		}
	}
	if config.FallbackMode == "" {
		config.FallbackMode = "ask"
	}
	if config.MCPIsolation == "" {
		config.MCPIsolation = "strict"
	}
	return config
}

func (c Config) Validate() error {
	if c.RetryMode == "" {
		c.RetryMode = "limited_auto"
	}
	if c.RetryMode != "limited_auto" && c.RetryMode != "ask" && c.RetryMode != "disabled" {
		return fmt.Errorf("unsupported retry_mode %q", c.RetryMode)
	}
	if c.MaxAutoRetries < 0 {
		return errors.New("max_auto_retries cannot be negative")
	}
	if c.FallbackMode == "" {
		c.FallbackMode = "ask"
	}
	if c.FallbackMode != "ask" && c.FallbackMode != "auto_registered" && c.FallbackMode != "disabled" {
		return fmt.Errorf("unsupported fallback_mode %q", c.FallbackMode)
	}
	if c.MCPIsolation == "" {
		c.MCPIsolation = "strict"
	}
	if c.MCPIsolation != "strict" && c.MCPIsolation != "config-isolated" && c.MCPIsolation != "unsupported" {
		return fmt.Errorf("unsupported mcp_isolation %q", c.MCPIsolation)
	}
	for _, binding := range c.RegisteredBindings {
		if err := validateBindingShape(binding); err != nil {
			return fmt.Errorf("registered binding: %w", err)
		}
	}
	return nil
}

func (c Config) Preflight(task orchestration.Task, requested orchestration.Binding) PreflightDecision {
	c = WithDefaults(c)
	decision := PreflightDecision{Status: "blocked", Code: "binding_not_registered", MCPIsolation: c.mcpIsolation(), Binding: requested}
	if err := validateBindingShape(requested); err != nil {
		decision.Code = "binding_incomplete"
		decision.Reason = err.Error()
		return decision
	}
	taskWorkspace, err := orchestration.CanonicalDirectory(task.Workspace)
	if err != nil {
		decision.Code = "workspace_untrusted"
		decision.Reason = err.Error()
		return decision
	}
	if requested.Workspace == "" {
		requested.Workspace = taskWorkspace
		decision.Binding = requested
	} else {
		canonicalRequested, canonicalErr := orchestration.CanonicalDirectory(requested.Workspace)
		if canonicalErr != nil {
			decision.Code = "workspace_untrusted"
			decision.Reason = canonicalErr.Error()
			return decision
		}
		requested.Workspace = canonicalRequested
		decision.Binding = requested
	}
	if filepath.Clean(requested.Workspace) != filepath.Clean(taskWorkspace) {
		decision.Code = "workspace_mismatch"
		decision.Reason = "binding workspace does not match task workspace"
		return decision
	}
	if len(requested.MCPAllowlist) > 0 {
		switch c.mcpIsolation() {
		case "unsupported":
			decision.Code = "mcp_isolation_unsupported"
			decision.Reason = "the selected execution path cannot isolate the task MCP allowlist"
			return decision
		case "strict":
			decision.Reason = "strict task/session MCP allowlist"
		case "config-isolated":
			decision.Reason = "task MCP allowlist is isolated through an explicit config"
		}
	}
	for _, registered := range c.RegisteredBindings {
		candidate := registered
		if candidate.Workspace == "" {
			candidate.Workspace = taskWorkspace
		} else if canonicalCandidate, canonicalErr := orchestration.CanonicalDirectory(candidate.Workspace); canonicalErr == nil {
			candidate.Workspace = canonicalCandidate
		}
		if sameBinding(candidate, requested) {
			decision.Status = "passed"
			decision.Code = ""
			decision.Reason = "registered_binding"
			decision.Binding = candidate
			return decision
		}
	}
	decision.Reason = "role, adapter, model, reasoning, permission and workspace must match a registered binding"
	return decision
}

func (c Config) RetryAllowed(attempt orchestration.Attempt, errorCode string, retriesAlreadyUsed int) bool {
	c = WithDefaults(c)
	if c.RetryMode == "disabled" || c.RetryMode == "ask" || retriesAlreadyUsed < 0 || retriesAlreadyUsed >= c.MaxAutoRetries {
		return false
	}
	if attempt.Status != orchestration.AttemptFailed && attempt.Status != orchestration.AttemptIncomplete && attempt.Status != orchestration.AttemptTimedOut {
		return false
	}
	if attempt.Status == orchestration.AttemptContaminated || attempt.Status == orchestration.AttemptCancelRequested {
		return false
	}
	if !attempt.ExecutionStarted && strings.EqualFold(attempt.SideEffectState, "none") {
		return contains([]string{"launch_not_started", "transport_before_session", "rate_limited_before_session"}, errorCode)
	}
	if attempt.ExecutionStarted && strings.EqualFold(attempt.SideEffectState, "none") && attempt.ReadOnlyIsolation && attempt.Binding.Permission == "read-only" {
		return contains([]string{"transport_after_start_readonly", "provider_transient_after_start_readonly", "host_disconnect_after_start_readonly"}, errorCode)
	}
	return false
}

func (c Config) DecideFallback(original, candidate orchestration.Binding, approved bool) FallbackDecision {
	c = WithDefaults(c)
	if c.FallbackMode == "disabled" {
		return FallbackDecision{Status: "blocked", Code: "fallback_disabled", Reason: "fallback is disabled by policy"}
	}
	if err := validateBindingShape(candidate); err != nil {
		return FallbackDecision{Status: "blocked", Code: "fallback_binding_incomplete", Reason: err.Error()}
	}
	if candidate.Workspace == "" {
		candidate.Workspace = original.Workspace
	}
	if !fallbackBindingShapeMatches(original, candidate) {
		return FallbackDecision{Status: "blocked", Code: "fallback_scope_changed", Reason: "fallback must preserve role, permission and workspace"}
	}
	registered := false
	for _, binding := range c.RegisteredBindings {
		if binding.Workspace == "" {
			binding.Workspace = original.Workspace
		}
		if sameBinding(binding, candidate) {
			registered = true
			break
		}
	}
	if !registered {
		return FallbackDecision{Status: "blocked", Code: "fallback_not_registered", Reason: "candidate fallback binding is not registered"}
	}
	if c.FallbackMode == "ask" && !approved {
		return FallbackDecision{Status: "awaiting_human_decision", Code: "fallback_approval_required", Reason: "fallback mode requires per-attempt approval"}
	}
	return FallbackDecision{Status: "passed", Code: "registered_fallback", Reason: "fallback binding is registered and approved by policy"}
}

func validateBindingShape(binding orchestration.Binding) error {
	fields := map[string]string{
		"role": binding.Role, "adapter": binding.Adapter, "model": binding.Model,
		"reasoning": binding.Reasoning, "permission": binding.Permission,
	}
	for name, value := range fields {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if binding.Workspace != "" && !filepath.IsAbs(binding.Workspace) {
		return errors.New("workspace must be an absolute path")
	}
	if err := validateMCPAllowlist(binding.MCPAllowlist); err != nil {
		return err
	}
	return nil
}

func validateMCPAllowlist(allowlist []string) error {
	seen := make(map[string]struct{}, len(allowlist))
	for _, server := range allowlist {
		server = strings.TrimSpace(server)
		if server == "" || server == "*" {
			return errors.New("mcp allowlist entries must be explicit non-wildcard names")
		}
		if _, ok := seen[server]; ok {
			return fmt.Errorf("mcp allowlist contains duplicate server %q", server)
		}
		seen[server] = struct{}{}
	}
	return nil
}

func (c Config) mcpIsolation() string {
	if strings.TrimSpace(c.MCPIsolation) == "" {
		return "strict"
	}
	return c.MCPIsolation
}

// ValidateGoalScope keeps Goal authorization tied to an explicit workflow
// node label or node id. Empty, wildcard and duplicate scopes fail closed.
func ValidateGoalScope(scope []string) error {
	if len(scope) == 0 {
		return errors.New("goal scope is required")
	}
	seen := make(map[string]struct{}, len(scope))
	for _, value := range scope {
		value = strings.TrimSpace(value)
		if value == "" || value == "*" {
			return errors.New("goal scope entries must be explicit non-wildcard values")
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("goal scope contains duplicate value %q", value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func GoalScopeAllows(scope []string, node orchestration.WorkflowNode) bool {
	for _, value := range scope {
		if strings.TrimSpace(value) == node.Kind || strings.TrimSpace(value) == node.ID {
			return true
		}
	}
	return false
}

func sameBinding(left, right orchestration.Binding) bool {
	return left.Role == right.Role && left.Adapter == right.Adapter && left.Model == right.Model &&
		left.Reasoning == right.Reasoning && left.Permission == right.Permission &&
		filepath.Clean(left.Workspace) == filepath.Clean(right.Workspace) && equalStrings(left.MCPAllowlist, right.MCPAllowlist)
}

func fallbackBindingShapeMatches(original, candidate orchestration.Binding) bool {
	return original.Role == candidate.Role && original.Permission == candidate.Permission && filepath.Clean(original.Workspace) == filepath.Clean(candidate.Workspace)
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func LoadJSON(raw []byte) (Config, error) {
	config := DefaultConfig()
	if err := json.Unmarshal(raw, &config); err != nil {
		return Config{}, err
	}
	config = WithDefaults(config)
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}
