package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agent-workflow-toolkit/internal/adapters/claude"
	"agent-workflow-toolkit/internal/adapters/codexcli"
	"agent-workflow-toolkit/internal/adapters/cursor"
	"agent-workflow-toolkit/internal/orchestration"
)

type PreflightResult struct {
	Provider    string `json:"provider"`
	ProfileID   string `json:"profile_id"`
	ProfileHash string `json:"profile_hash"`
	Status      string `json:"status"`
	Code        string `json:"code,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Version     string `json:"version,omitempty"`
}

type Runtime struct {
	provider string
	profile  Profile
	attempt  orchestration.Attempt
	handle   string
	session  string
	claude   *claude.Adapter
	cursor   *cursor.Adapter
	codex    *codexcli.Adapter
}

func NewRuntime(request Request) (*Runtime, error) {
	if err := validateRequest(request); err != nil {
		return nil, err
	}
	profile := request.Profile
	if err := validateProfile(profile); err != nil {
		return nil, err
	}
	if request.ExpectedProfileHash != "" && !strings.EqualFold(request.ExpectedProfileHash, profile.ProfileHash) {
		return nil, errors.New("adapter profile hash mismatch")
	}
	if !strings.EqualFold(profile.ProfileHash, orchestration.HashAdapterProfile(profile.AdapterProfile)) {
		return nil, errors.New("adapter profile content hash mismatch")
	}
	if err := validateAction(request.Action, request.Attempt); err != nil {
		return nil, err
	}
	if request.Action.Kind == orchestration.ActionLaunch {
		if err := validateLaunchContract(request.Action, request.Attempt, request.RolePromptHash); err != nil {
			return nil, err
		}
	}
	runtime := &Runtime{provider: normalizeProvider(request.Attempt.Binding.Adapter), profile: profile, attempt: request.Attempt}
	return runtime, nil
}

func (r *Runtime) Execute(ctx context.Context, action orchestration.HostAction) (orchestration.HostActionResult, error) {
	if r == nil {
		return orchestration.HostActionResult{}, errors.New("bridge runtime is nil")
	}
	if ctx == nil {
		return orchestration.HostActionResult{}, errors.New("context is required")
	}
	if err := validateAction(action, r.attempt); err != nil {
		return orchestration.HostActionResult{}, err
	}
	switch action.Kind {
	case orchestration.ActionLaunch:
		return r.launch(ctx, action)
	case orchestration.ActionObserve:
		return r.observe(ctx, action)
	case orchestration.ActionCancelHost:
		return r.cancel(ctx, action)
	default:
		return orchestration.HostActionResult{}, fmt.Errorf("unsupported bridge action %q", action.Kind)
	}
}

func (r *Runtime) Cleanup() error {
	if r == nil || r.handle == "" {
		return nil
	}
	switch r.provider {
	case ProviderClaude:
		if r.claude != nil {
			return r.claude.Cleanup(claude.CleanupRequest{HandleID: r.handle})
		}
	case ProviderCursor:
		if r.cursor != nil {
			return r.cursor.Cleanup(cursor.CleanupRequest{HandleID: r.handle})
		}
	case ProviderCodexCLI:
		if r.codex != nil {
			return r.codex.Cleanup(codexcli.CleanupRequest{HandleID: r.handle})
		}
	}
	return nil
}

func (r *Runtime) Handle() string {
	if r == nil {
		return ""
	}
	return r.handle
}

func (r *Runtime) Session() string {
	if r == nil {
		return ""
	}
	return r.session
}

func AdapterPreflight(ctx context.Context, binding orchestration.Binding, profile Profile) (PreflightResult, error) {
	if ctx == nil {
		return PreflightResult{}, errors.New("context is required")
	}
	if err := validateProfile(profile); err != nil {
		return PreflightResult{}, err
	}
	provider := normalizeProvider(binding.Adapter)
	if profile.Adapter != provider {
		return PreflightResult{}, errors.New("adapter profile does not match binding adapter")
	}
	result := PreflightResult{Provider: provider, ProfileID: profile.ID, ProfileHash: profile.ProfileHash}
	switch provider {
	case ProviderClaude:
		adapter := claude.New(profile.Executable)
		preflight, err := adapter.Preflight(ctx, claude.PreflightRequest{Workspace: binding.Workspace, Model: binding.Model, Reasoning: binding.Reasoning, Permission: binding.Permission, TrustAuthorized: profile.TrustAuthorized, MCPIsolation: "strict", MCPAllowlist: binding.MCPAllowlist, MCPConfigPath: profile.MCPConfigPath})
		if err != nil {
			return PreflightResult{}, err
		}
		result.Status, result.Code, result.Reason, result.Version = preflight.Status, preflight.Code, preflight.Reason, preflight.Version
	case ProviderCursor:
		adapter := cursor.New(profile.Executable)
		mode := profile.Mode
		if mode == "" {
			mode = "plan"
		}
		preflight, err := adapter.Preflight(ctx, cursor.PreflightRequest{Workspace: binding.Workspace, Model: binding.Model, Mode: mode, Permission: binding.Permission, TrustAuthorized: profile.TrustAuthorized, MCPIsolation: "strict", MCPAllowlist: binding.MCPAllowlist})
		if err != nil {
			return PreflightResult{}, err
		}
		result.Status, result.Code, result.Reason, result.Version = preflight.Status, preflight.Code, preflight.Reason, preflight.Version
	case ProviderCodexCLI:
		adapter := configuredCodex(profile)
		preflight, err := adapter.Preflight(ctx, codexcli.PreflightRequest{Workspace: binding.Workspace, Model: binding.Model, Reasoning: binding.Reasoning, Permission: binding.Permission, Sandbox: profile.Sandbox, Approval: profile.Approval, TrustAuthorized: profile.TrustAuthorized, MCPIsolation: "strict", MCPAllowlist: binding.MCPAllowlist})
		if err != nil {
			return PreflightResult{}, err
		}
		result.Status, result.Code, result.Reason, result.Version = preflight.Status, preflight.Code, preflight.Reason, preflight.Version
	default:
		return PreflightResult{}, fmt.Errorf("unsupported external CLI adapter %q", provider)
	}
	return result, nil
}

func (r *Runtime) launch(ctx context.Context, action orchestration.HostAction) (orchestration.HostActionResult, error) {
	prompt := actionString(action.Parameters, "dispatch_prompt")
	if prompt == "" {
		return orchestration.HostActionResult{}, errors.New("core-issued launch prompt is required")
	}
	var result orchestration.HostActionResult
	switch r.provider {
	case ProviderClaude:
		if r.claude == nil {
			r.claude = claude.New(r.profile.Executable)
		}
		adapterResult, adapterErr := r.claude.Launch(ctx, claude.LaunchRequest{Workspace: r.attempt.Binding.Workspace, Model: r.attempt.Binding.Model, Reasoning: r.attempt.Binding.Reasoning, Permission: r.attempt.Binding.Permission, TrustAuthorized: r.profile.TrustAuthorized, MCPIsolation: r.attempt.MCPIsolation, MCPAllowlist: r.attempt.Binding.MCPAllowlist, MCPConfigPath: r.profile.MCPConfigPath, Prompt: prompt, TaskCanary: r.attempt.TaskCanary})
		if adapterErr != nil {
			return providerFailure(adapterErr), adapterErr
		}
		result = adapterResult.HostActionResult()
	case ProviderCursor:
		if r.cursor == nil {
			r.cursor = cursor.New(r.profile.Executable)
		}
		mode := r.profile.Mode
		if mode == "" {
			mode = "plan"
		}
		adapterResult, adapterErr := r.cursor.Launch(ctx, cursor.LaunchRequest{Workspace: r.attempt.Binding.Workspace, Model: r.attempt.Binding.Model, Mode: mode, Permission: r.attempt.Binding.Permission, TrustAuthorized: r.profile.TrustAuthorized, MCPIsolation: r.attempt.MCPIsolation, MCPAllowlist: r.attempt.Binding.MCPAllowlist, Prompt: prompt, TaskCanary: r.attempt.TaskCanary})
		if adapterErr != nil {
			return providerFailure(adapterErr), adapterErr
		}
		result = adapterResult.HostActionResult()
	case ProviderCodexCLI:
		r.codex = configuredCodex(r.profile)
		adapterResult, adapterErr := r.codex.Launch(ctx, codexcli.LaunchRequest{Workspace: r.attempt.Binding.Workspace, Model: r.attempt.Binding.Model, Reasoning: r.attempt.Binding.Reasoning, Permission: r.attempt.Binding.Permission, Sandbox: r.profile.Sandbox, Approval: r.profile.Approval, TrustAuthorized: r.profile.TrustAuthorized, MCPIsolation: r.attempt.MCPIsolation, MCPAllowlist: r.attempt.Binding.MCPAllowlist, Prompt: prompt, TaskCanary: r.attempt.TaskCanary})
		if adapterErr != nil {
			return providerFailure(adapterErr), adapterErr
		}
		result = adapterResult.HostActionResult()
	default:
		return orchestration.HostActionResult{}, fmt.Errorf("unsupported external CLI adapter %q", r.provider)
	}
	r.handle, r.session = result.ProviderHandle, result.ProviderSessionID
	return result, nil
}

func (r *Runtime) observe(ctx context.Context, action orchestration.HostAction) (orchestration.HostActionResult, error) {
	if r.handle == "" {
		return orchestration.HostActionResult{}, errors.New("stale_owner: provider handle is unavailable")
	}
	if expected := actionString(action.Parameters, "provider_handle"); expected != "" && expected != r.handle {
		return orchestration.HostActionResult{}, errors.New("stale_owner: provider handle does not match the owner")
	}
	switch r.provider {
	case ProviderClaude:
		if r.claude == nil {
			return orchestration.HostActionResult{}, errors.New("stale_owner: Claude Adapter is unavailable")
		}
		result, err := r.claude.Observe(ctx, claude.ObserveRequest{HandleID: r.handle})
		if err != nil {
			return orchestration.HostActionResult{}, fmt.Errorf("stale_owner: %w", err)
		}
		return result.HostActionResult(), nil
	case ProviderCursor:
		if r.cursor == nil {
			return orchestration.HostActionResult{}, errors.New("stale_owner: Cursor Adapter is unavailable")
		}
		result, err := r.cursor.Observe(ctx, cursor.ObserveRequest{HandleID: r.handle})
		if err != nil {
			return orchestration.HostActionResult{}, fmt.Errorf("stale_owner: %w", err)
		}
		return result.HostActionResult(), nil
	case ProviderCodexCLI:
		if r.codex == nil {
			return orchestration.HostActionResult{}, errors.New("stale_owner: Codex CLI Adapter is unavailable")
		}
		result, err := r.codex.Observe(ctx, codexcli.ObserveRequest{HandleID: r.handle})
		if err != nil {
			return orchestration.HostActionResult{}, fmt.Errorf("stale_owner: %w", err)
		}
		return result.HostActionResult(), nil
	default:
		return orchestration.HostActionResult{}, fmt.Errorf("unsupported external CLI adapter %q", r.provider)
	}
}

func (r *Runtime) cancel(ctx context.Context, action orchestration.HostAction) (orchestration.HostActionResult, error) {
	if r.handle == "" {
		return orchestration.HostActionResult{}, errors.New("stale_owner: provider handle is unavailable")
	}
	if expected := actionString(action.Parameters, "provider_handle"); expected != "" && expected != r.handle {
		return orchestration.HostActionResult{}, errors.New("stale_owner: provider handle does not match the owner")
	}
	switch r.provider {
	case ProviderClaude:
		if r.claude == nil {
			return orchestration.HostActionResult{}, errors.New("stale_owner: Claude Adapter is unavailable")
		}
		result, err := r.claude.Cancel(ctx, claude.CancelRequest{HandleID: r.handle})
		if err != nil {
			return orchestration.HostActionResult{}, fmt.Errorf("stale_owner: %w", err)
		}
		return result.HostActionResult(), nil
	case ProviderCursor:
		if r.cursor == nil {
			return orchestration.HostActionResult{}, errors.New("stale_owner: Cursor Adapter is unavailable")
		}
		result, err := r.cursor.Cancel(ctx, cursor.CancelRequest{HandleID: r.handle})
		if err != nil {
			return orchestration.HostActionResult{}, fmt.Errorf("stale_owner: %w", err)
		}
		return result.HostActionResult(), nil
	case ProviderCodexCLI:
		if r.codex == nil {
			return orchestration.HostActionResult{}, errors.New("stale_owner: Codex CLI Adapter is unavailable")
		}
		result, err := r.codex.Cancel(ctx, codexcli.CancelRequest{HandleID: r.handle})
		if err != nil {
			return orchestration.HostActionResult{}, fmt.Errorf("stale_owner: %w", err)
		}
		return result.HostActionResult(), nil
	default:
		return orchestration.HostActionResult{}, fmt.Errorf("unsupported external CLI adapter %q", r.provider)
	}
}

func configuredCodex(profile Profile) *codexcli.Adapter {
	adapter := codexcli.New(profile.Executable)
	adapter.ModelProvider = profile.ProviderRef
	adapter.ProviderBaseURL = profile.ProviderBaseURL
	adapter.ProviderWireAPI = profile.ProviderWireAPI
	adapter.ProviderAuth = profile.ProviderAuth
	return adapter
}

func validateRequest(request Request) error {
	if strings.TrimSpace(request.AttemptID) == "" || request.Attempt.ID != request.AttemptID {
		return errors.New("attempt identity is required and must match the request")
	}
	if request.Action.AttemptID != request.AttemptID || request.Action.ID == "" {
		return errors.New("core-issued action identity is invalid")
	}
	if strings.TrimSpace(request.Attempt.Binding.Adapter) == "" || strings.TrimSpace(request.Attempt.Binding.Workspace) == "" {
		return errors.New("attempt binding adapter and workspace are required")
	}
	if strings.TrimSpace(request.Profile.ID) == "" || strings.TrimSpace(request.Profile.Adapter) == "" {
		return errors.New("adapter profile identity is required")
	}
	if normalizeProvider(request.Profile.Adapter) != normalizeProvider(request.Attempt.Binding.Adapter) {
		return errors.New("adapter profile does not match the Attempt binding")
	}
	return nil
}

func validateProfile(profile Profile) error {
	if strings.TrimSpace(profile.ID) == "" || strings.TrimSpace(profile.Adapter) == "" || strings.TrimSpace(profile.ProfileHash) == "" {
		return errors.New("profile id, adapter and profile_hash are required")
	}
	if normalizeProvider(profile.Adapter) != profile.Adapter {
		return errors.New("profile adapter must use the canonical provider name")
	}
	return nil
}

func validateAction(action orchestration.HostAction, attempt orchestration.Attempt) error {
	if action.AttemptID != attempt.ID || action.ID == "" {
		return errors.New("core action identity mismatch")
	}
	if action.Status != orchestration.ActionPending {
		return errors.New("core action is not pending")
	}
	if action.ParametersHash != orchestration.HashParameters(action.Parameters) {
		return errors.New("core action parameters hash mismatch")
	}
	if action.Parameters["task_canary"] != attempt.TaskCanary {
		return errors.New("core action task canary mismatch")
	}
	if workspace := actionString(action.Parameters, "workspace"); workspace != "" && !samePath(workspace, attempt.Binding.Workspace) {
		return errors.New("core action workspace mismatch")
	}
	switch action.Kind {
	case orchestration.ActionLaunch, orchestration.ActionObserve, orchestration.ActionCancelHost:
		return nil
	default:
		return fmt.Errorf("unsupported bridge action %q", action.Kind)
	}
}

func validateLaunchContract(action orchestration.HostAction, attempt orchestration.Attempt, expectedHash string) error {
	prompt := actionString(action.Parameters, "dispatch_prompt")
	if prompt == "" {
		return errors.New("core launch action has no dispatch prompt")
	}
	if hash := actionString(action.Parameters, "dispatch_prompt_hash"); hash == "" || !strings.EqualFold(hash, hashText(prompt)) {
		return errors.New("core launch dispatch prompt hash mismatch")
	}
	if expectedHash != "" {
		actual := actionString(action.Parameters, "role_prompt_hash")
		if actual == "" || !strings.EqualFold(actual, expectedHash) {
			return errors.New("core launch Role Prompt hash mismatch")
		}
	}
	if path := actionString(action.Parameters, "role_prompt_path"); path != "" {
		if !filepath.IsAbs(path) {
			return errors.New("Role Prompt reference must be absolute")
		}
		contents, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return fmt.Errorf("read Role Prompt reference: %w", err)
		}
		if expected := actionString(action.Parameters, "role_prompt_hash"); expected == "" || !strings.EqualFold(expected, hashText(string(contents))) {
			return errors.New("Role Prompt reference hash mismatch")
		}
	}
	return nil
}

func providerFailure(err error) orchestration.HostActionResult {
	started := true
	return orchestration.HostActionResult{Status: "failed", Reason: "bridge_provider_error", Terminal: true, ExecutionStarted: &started, SideEffectState: "unknown", RawEventRefs: []string{"bridge:error"}}
}

func actionString(parameters map[string]any, key string) string {
	value, _ := parameters[key].(string)
	return strings.TrimSpace(value)
}

func hashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func normalizeProvider(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "claude-code", "claude_code":
		return ProviderClaude
	case "cursor-agent", "cursor_agent":
		return ProviderCursor
	case "codex-cli", "codex_cli":
		return ProviderCodexCLI
	default:
		return value
	}
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(filepath.Clean(left))
	rightAbs, rightErr := filepath.Abs(filepath.Clean(right))
	if leftErr != nil || rightErr != nil {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return strings.EqualFold(leftAbs, rightAbs)
}

func profileJSON(profile Profile) []byte {
	profile.ProfileHash = ""
	raw, _ := json.Marshal(profile.AdapterProfile)
	return raw
}
