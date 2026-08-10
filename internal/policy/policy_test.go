package policy

import (
	"os"
	"path/filepath"
	"testing"

	"agent-workflow-toolkit/internal/orchestration"
)

func TestPreflightRequiresExactRegisteredBinding(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	task, err := orchestration.NewTask("test", "review", workspace)
	if err != nil {
		t.Fatal(err)
	}
	binding := orchestration.Binding{Role: "reviewer", Adapter: "fake", Model: "fixture", Reasoning: "none", Permission: "read-only", Workspace: workspace}
	config := DefaultConfig()
	config.RegisteredBindings = []orchestration.Binding{binding}
	passed := config.Preflight(task, binding)
	if passed.Status != "passed" {
		t.Fatalf("preflight = %#v", passed)
	}
	wrongModel := binding
	wrongModel.Model = "unregistered"
	blocked := config.Preflight(task, wrongModel)
	if blocked.Status != "blocked" || blocked.Code != "binding_not_registered" {
		t.Fatalf("wrong model preflight = %#v", blocked)
	}
	wrongWorkspace := binding
	wrongWorkspace.Workspace = filepath.Join(t.TempDir(), "other")
	if err := os.Mkdir(wrongWorkspace.Workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	blocked = config.Preflight(task, wrongWorkspace)
	if blocked.Status != "blocked" || blocked.Code != "workspace_mismatch" {
		t.Fatalf("wrong workspace preflight = %#v", blocked)
	}
}

func TestRetryBranchesAreMutuallyExclusive(t *testing.T) {
	config := DefaultConfig()
	attempt := orchestration.Attempt{Status: orchestration.AttemptFailed, Binding: orchestration.Binding{Permission: "read-only"}, SideEffectState: "none", ReadOnlyIsolation: true, ExecutionStarted: true}
	if !config.RetryAllowed(attempt, "transport_after_start_readonly", 0) {
		t.Fatal("readonly post-start retry was rejected")
	}
	attempt.ReadOnlyIsolation = false
	if config.RetryAllowed(attempt, "transport_after_start_readonly", 0) {
		t.Fatal("post-start retry was allowed without mechanical readonly isolation")
	}
	attempt.ExecutionStarted = false
	attempt.SideEffectState = "none"
	if !config.RetryAllowed(attempt, "transport_before_session", 0) {
		t.Fatal("pre-start retry was rejected")
	}
	if config.RetryAllowed(attempt, "transport_after_start_readonly", 1) {
		t.Fatal("retry exceeded configured maximum")
	}
}

func TestFallbackPolicyRequiresRegistrationAndApproval(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	original := orchestration.Binding{Role: "reviewer", Adapter: "cursor", Model: "model-a", Reasoning: "none", Permission: "read-only", Workspace: workspace}
	candidate := original
	candidate.Adapter = "claude"
	candidate.Model = "model-b"
	config := DefaultConfig()
	config.RegisteredBindings = []orchestration.Binding{candidate}
	decision := config.DecideFallback(original, candidate, false)
	if decision.Status != "awaiting_human_decision" || decision.Code != "fallback_approval_required" {
		t.Fatalf("ask fallback without approval = %#v", decision)
	}
	decision = config.DecideFallback(original, candidate, true)
	if decision.Status != "passed" {
		t.Fatalf("approved registered fallback = %#v", decision)
	}
	config.FallbackMode = "disabled"
	decision = config.DecideFallback(original, candidate, true)
	if decision.Status != "blocked" || decision.Code != "fallback_disabled" {
		t.Fatalf("disabled fallback = %#v", decision)
	}
	config.FallbackMode = "auto_registered"
	wrongRole := candidate
	wrongRole.Role = "worker"
	decision = config.DecideFallback(original, wrongRole, true)
	if decision.Status != "blocked" || decision.Code != "fallback_scope_changed" {
		t.Fatalf("scope-changing fallback = %#v", decision)
	}
	registeredWithoutWorkspace := candidate
	registeredWithoutWorkspace.Workspace = ""
	candidateWithoutWorkspace := candidate
	candidateWithoutWorkspace.Workspace = ""
	config.RegisteredBindings = []orchestration.Binding{registeredWithoutWorkspace}
	decision = config.DecideFallback(original, candidateWithoutWorkspace, true)
	if decision.Status != "passed" {
		t.Fatalf("fallback workspace normalization = %#v", decision)
	}
}

func TestPolicyDefaultsMaterializeFailClosedFallbackAsk(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	candidate := orchestration.Binding{Role: "reviewer", Adapter: "fake", Model: "fixture", Reasoning: "none", Permission: "read-only", Workspace: workspace}
	config := WithDefaults(Config{RetryMode: "limited_auto", RegisteredBindings: []orchestration.Binding{candidate}})
	if config.FallbackMode != "ask" || config.MCPIsolation != "strict" {
		t.Fatalf("materialized defaults = %#v", config)
	}
	if decision := config.DecideFallback(candidate, candidate, false); decision.Status != "awaiting_human_decision" || decision.Code != "fallback_approval_required" {
		t.Fatalf("empty fallback default bypassed approval = %#v", decision)
	}
	loaded, err := LoadJSON([]byte(`{"retry_mode":"limited_auto","fallback_mode":"","mcp_isolation":""}`))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.FallbackMode != "ask" || loaded.MCPIsolation != "strict" {
		t.Fatalf("JSON defaults were not materialized = %#v", loaded)
	}
}

func TestMCPIsolationModesFailClosedForTaskAllowlist(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	task, err := orchestration.NewTask("mcp", "review", workspace)
	if err != nil {
		t.Fatal(err)
	}
	binding := orchestration.Binding{Role: "reviewer", Adapter: "fake", Model: "fixture", Reasoning: "none", Permission: "read-only", Workspace: workspace, MCPAllowlist: []string{"evidence"}}
	for _, mode := range []string{"strict", "config-isolated"} {
		config := DefaultConfig()
		config.MCPIsolation = mode
		config.RegisteredBindings = []orchestration.Binding{binding}
		if decision := config.Preflight(task, binding); decision.Status != "passed" || decision.MCPIsolation != mode {
			t.Fatalf("mcp mode %q preflight = %#v", mode, decision)
		}
	}
	unsupported := DefaultConfig()
	unsupported.MCPIsolation = "unsupported"
	unsupported.RegisteredBindings = []orchestration.Binding{binding}
	if decision := unsupported.Preflight(task, binding); decision.Status != "blocked" || decision.Code != "mcp_isolation_unsupported" {
		t.Fatalf("unsupported mcp mode preflight = %#v", decision)
	}
	bad := binding
	bad.MCPAllowlist = []string{"*"}
	if err := (Config{RegisteredBindings: []orchestration.Binding{bad}}).Validate(); err == nil {
		t.Fatal("wildcard MCP allowlist was accepted")
	}
}

func TestPreflightRejectsMissingWorkspaceAndGoalScopeRejectsWildcards(t *testing.T) {
	missing := orchestration.Task{Workspace: filepath.Join(t.TempDir(), "missing")}
	binding := orchestration.Binding{Role: "reviewer", Adapter: "fake", Model: "fixture", Reasoning: "none", Permission: "read-only", Workspace: missing.Workspace}
	if decision := DefaultConfig().Preflight(missing, binding); decision.Status != "blocked" || decision.Code != "workspace_untrusted" {
		t.Fatalf("missing workspace preflight = %#v", decision)
	}
	for _, scope := range [][]string{nil, {"*"}, {"review", "review"}, {""}} {
		if err := ValidateGoalScope(scope); err == nil {
			t.Fatalf("invalid goal scope accepted: %#v", scope)
		}
	}
}

func TestWave4PolicyMatrixFailsClosed(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	task, err := orchestration.NewTask("policy-matrix", "review", workspace)
	if err != nil {
		t.Fatal(err)
	}
	registered := orchestration.Binding{Role: "reviewer", Adapter: "fake", Model: "fixture", Reasoning: "none", Permission: "read-only", Workspace: workspace}

	cases := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "unregistered binding",
			check: func(t *testing.T) {
				candidate := registered
				candidate.Model = "unregistered"
				decision := (Config{RegisteredBindings: []orchestration.Binding{registered}}).Preflight(task, candidate)
				if decision.Status != "blocked" || decision.Code != "binding_not_registered" {
					t.Fatalf("decision = %#v", decision)
				}
			},
		},
		{
			name: "permission expansion",
			check: func(t *testing.T) {
				candidate := registered
				candidate.Permission = "workspace-write"
				decision := (Config{RegisteredBindings: []orchestration.Binding{registered}}).Preflight(task, candidate)
				if decision.Status != "blocked" || decision.Code != "binding_not_registered" {
					t.Fatalf("decision = %#v", decision)
				}
			},
		},
		{
			name: "MCP expansion without isolation",
			check: func(t *testing.T) {
				candidate := registered
				candidate.MCPAllowlist = []string{"evidence"}
				config := Config{MCPIsolation: "unsupported", RegisteredBindings: []orchestration.Binding{candidate}}
				decision := config.Preflight(task, candidate)
				if decision.Status != "blocked" || decision.Code != "mcp_isolation_unsupported" {
					t.Fatalf("decision = %#v", decision)
				}
			},
		},
		{
			name: "goal scope overreach",
			check: func(t *testing.T) {
				node := orchestration.WorkflowNode{ID: "node-review", Kind: "review"}
				if GoalScopeAllows([]string{"worker"}, node) {
					t.Fatal("out-of-scope goal was accepted")
				}
				if err := ValidateGoalScope([]string{"*"}); err == nil {
					t.Fatal("wildcard goal scope was accepted")
				}
			},
		},
		{
			name: "fallback scope expansion",
			check: func(t *testing.T) {
				candidate := registered
				candidate.Permission = "workspace-write"
				decision := (Config{RegisteredBindings: []orchestration.Binding{candidate}}).DecideFallback(registered, candidate, true)
				if decision.Status != "blocked" || decision.Code != "fallback_scope_changed" {
					t.Fatalf("decision = %#v", decision)
				}
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, testCase.check)
	}
}

func TestRetryPolicyRejectsWrongEvidenceAndAllowsOnlyRegisteredBranches(t *testing.T) {
	config := DefaultConfig()
	preStart := orchestration.Attempt{Status: orchestration.AttemptFailed, ExecutionStarted: false, SideEffectState: "none"}
	if !config.RetryAllowed(preStart, "transport_before_session", 0) {
		t.Fatal("pre-start evidence was rejected")
	}
	if config.RetryAllowed(preStart, "transport_after_start_readonly", 0) {
		t.Fatal("post-start error crossed into pre-start branch")
	}
	postStart := orchestration.Attempt{Status: orchestration.AttemptIncomplete, ExecutionStarted: true, SideEffectState: "none", ReadOnlyIsolation: true, Binding: orchestration.Binding{Permission: "read-only"}}
	if !config.RetryAllowed(postStart, "provider_transient_after_start_readonly", 0) {
		t.Fatal("post-start readonly evidence was rejected")
	}
	postStart.SideEffectState = "unknown"
	if config.RetryAllowed(postStart, "provider_transient_after_start_readonly", 0) {
		t.Fatal("unknown side-effect evidence was accepted")
	}
	postStart.SideEffectState = "none"
	postStart.ReadOnlyIsolation = false
	if config.RetryAllowed(postStart, "provider_transient_after_start_readonly", 0) {
		t.Fatal("non-isolated post-start execution was accepted")
	}
}
