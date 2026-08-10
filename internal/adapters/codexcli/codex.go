// Package codexcli contains the Codex CLI process and delivery boundary.
//
// The Adapter treats the CLI process, JSONL stream and durable
// --output-last-message file as separate evidence sources. A zero exit code
// is never sufficient for completion on its own.
package codexcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"agent-workflow-toolkit/internal/adapters/artifact"
	"agent-workflow-toolkit/internal/adapters/cli"
	"agent-workflow-toolkit/internal/orchestration"
)

const (
	Provider              = "codexcli"
	DefaultExecutable     = "codex"
	DefaultSandbox        = "read-only"
	DefaultApproval       = "untrusted"
	DefaultPermission     = "read-only"
	DefaultStartupTimeout = 30 * time.Second
)

type CapabilityStatus string

const (
	CapabilitySupported   CapabilityStatus = "supported"
	CapabilityUnsupported CapabilityStatus = "unsupported"
	CapabilityDegraded    CapabilityStatus = "degraded"
)

type Capability struct {
	Status CapabilityStatus `json:"status"`
	Reason string           `json:"reason,omitempty"`
}

type Capabilities struct {
	Launch         Capability `json:"launch"`
	Observe        Capability `json:"observe"`
	Send           Capability `json:"send"`
	Cancel         Capability `json:"cancel"`
	Cleanup        Capability `json:"cleanup"`
	WorkspaceTrust Capability `json:"workspace_trust"`
	Permission     Capability `json:"permission"`
	Sandbox        Capability `json:"sandbox"`
	MCP            Capability `json:"mcp"`
	ModelProbe     Capability `json:"model_probe"`
	Artifact       Capability `json:"artifact"`
}

func (a *Adapter) Capabilities() Capabilities {
	return Capabilities{
		Launch:         Capability{Status: CapabilitySupported, Reason: "argv-only Codex exec launch with JSONL capture"},
		Observe:        Capability{Status: CapabilitySupported, Reason: "retained process/thread stream observation"},
		Send:           Capability{Status: CapabilitySupported, Reason: "resume requires a core-authorized action and verified thread identity"},
		Cancel:         Capability{Status: CapabilitySupported, Reason: "process-tree cancellation with explicit unconfirmed state"},
		Cleanup:        Capability{Status: CapabilitySupported, Reason: "session reference cleanup after terminal process"},
		WorkspaceTrust: Capability{Status: CapabilitySupported, Reason: "launch requires explicit caller authorization before CLI execution"},
		Permission:     Capability{Status: CapabilitySupported, Reason: "read-only maps to the explicit Codex read-only sandbox"},
		Sandbox:        Capability{Status: CapabilitySupported, Reason: "read-only sandbox is passed explicitly; write modes are not admitted in Wave 3"},
		MCP:            Capability{Status: CapabilityUnsupported, Reason: "task/session MCP allowlist isolation is not exposed by the verified Codex CLI contract"},
		ModelProbe:     Capability{Status: CapabilityDegraded, Reason: "Codex CLI exposes no verified model-list command; the explicit binding is preserved and provider validates it at launch"},
		Artifact:       Capability{Status: CapabilitySupported, Reason: "core-assigned workspace artifact staging"},
	}
}

type ProbeFunc func(context.Context, string, []string) (cli.Result, error)

// Adapter owns external Codex processes, thread identity and delivery
// evidence. Runner and Probe are injectable for deterministic tests.
type Adapter struct {
	Executable       string
	Environment      []string
	ModelProvider    string
	ProviderBaseURL  string
	ProviderWireAPI  string
	ProviderAuth     bool
	Runner           *cli.Runner
	Probe            ProbeFunc
	TranscriptWriter func(string, []byte) error
	StartupTimeout   time.Duration

	mu       sync.Mutex
	sessions map[string]*session
}

func New(executable string) *Adapter {
	if strings.TrimSpace(executable) == "" {
		executable = DefaultExecutable
	}
	return &Adapter{Executable: executable, Runner: cli.NewRunner(), StartupTimeout: DefaultStartupTimeout, sessions: map[string]*session{}}
}

type DeliveryStatus string

const (
	StatusCompleted       DeliveryStatus = "completed"
	StatusDeliveryMissing DeliveryStatus = "incomplete"
	StatusFailed          DeliveryStatus = "failed"
)

type Invocation struct {
	Executable            string
	Workspace             string
	Model                 string
	ModelProvider         string
	ProviderBaseURL       string
	ProviderWireAPI       string
	ProviderAuth          bool
	Reasoning             string
	Sandbox               string
	Approval              string
	OutputLastMessagePath string
	Ephemeral             bool
	ResumeSessionID       string
	Environment           []string
}

func (i Invocation) Validate() error {
	if strings.TrimSpace(i.Executable) == "" {
		return errors.New("codex executable is required")
	}
	if strings.TrimSpace(i.Workspace) == "" || !filepath.IsAbs(i.Workspace) {
		return errors.New("an absolute workspace path is required")
	}
	if strings.TrimSpace(i.Model) == "" {
		return errors.New("registered model is required")
	}
	if strings.TrimSpace(i.OutputLastMessagePath) == "" || !filepath.IsAbs(i.OutputLastMessagePath) {
		return errors.New("an absolute output-last-message path is required")
	}
	if strings.TrimSpace(i.ResumeSessionID) != "" && i.Ephemeral {
		return errors.New("an ephemeral Codex invocation cannot resume a session")
	}
	if _, err := normalizeCodexReasoning(i.Reasoning); err != nil {
		return err
	}
	if err := validateProviderConfig(i.ModelProvider, i.ProviderBaseURL, i.ProviderWireAPI); err != nil {
		return err
	}
	sandbox := i.Sandbox
	if sandbox == "" {
		sandbox = DefaultSandbox
	}
	if sandbox != "read-only" && sandbox != "workspace-write" && sandbox != "danger-full-access" {
		return fmt.Errorf("unsupported Codex sandbox %q", sandbox)
	}
	approval := i.Approval
	if approval == "" {
		approval = DefaultApproval
	}
	if approval != "untrusted" && approval != "on-request" && approval != "never" {
		return fmt.Errorf("unsupported Codex approval policy %q", approval)
	}
	return nil
}

// normalizeCodexReasoning deliberately validates the value as a single argv
// token instead of turning provider effort levels into a closed repository
// enum. Codex may add effort names; the provider remains responsible for
// accepting or rejecting a registered value.
func normalizeCodexReasoning(reasoning string) (string, error) {
	reasoning = strings.ToLower(strings.TrimSpace(reasoning))
	if reasoning == "" || reasoning == "none" {
		return "", nil
	}
	if len(reasoning) > 64 {
		return "", errors.New("Codex reasoning value is too long")
	}
	for _, character := range reasoning {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '.' || character == '_' || character == '-' {
			continue
		}
		return "", fmt.Errorf("Codex reasoning value %q is not a safe provider token", reasoning)
	}
	return reasoning, nil
}

func validateProviderConfig(provider, baseURL, wireAPI string) error {
	provider = strings.TrimSpace(provider)
	baseURL = strings.TrimSpace(baseURL)
	wireAPI = strings.TrimSpace(wireAPI)
	if provider == "" {
		if baseURL != "" || wireAPI != "" {
			return errors.New("Codex provider base URL and wire API require a model provider")
		}
		return nil
	}
	if _, err := normalizeCodexReasoning(provider); err != nil {
		return fmt.Errorf("invalid Codex model provider: %w", err)
	}
	if baseURL == "" {
		return errors.New("Codex model provider base URL is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return errors.New("Codex provider base URL must be an absolute URL without embedded credentials")
	}
	if wireAPI != "" {
		if _, err := normalizeCodexReasoning(wireAPI); err != nil {
			return fmt.Errorf("invalid Codex provider wire API: %w", err)
		}
	}
	return nil
}

func (i Invocation) Args(prompt string) ([]string, error) {
	if err := i.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("prompt is required")
	}
	sandbox := i.Sandbox
	if sandbox == "" {
		sandbox = DefaultSandbox
	}
	approval := i.Approval
	if approval == "" {
		approval = DefaultApproval
	}
	args := []string{
		"--ask-for-approval", approval,
		"--sandbox", sandbox,
		"--cd", i.Workspace,
		"--model", i.Model,
	}
	if reasoning, _ := normalizeCodexReasoning(i.Reasoning); reasoning != "" {
		args = append(args, "--config", fmt.Sprintf("model_reasoning_effort=%q", reasoning))
	}
	if provider := strings.TrimSpace(i.ModelProvider); provider != "" {
		args = append(args, "--config", fmt.Sprintf("model_provider=%q", provider))
		args = append(args, "--config", fmt.Sprintf("model_providers.%s.name=%q", provider, provider))
		args = append(args, "--config", fmt.Sprintf("model_providers.%s.base_url=%q", provider, strings.TrimSpace(i.ProviderBaseURL)))
		if wireAPI := strings.TrimSpace(i.ProviderWireAPI); wireAPI != "" {
			args = append(args, "--config", fmt.Sprintf("model_providers.%s.wire_api=%q", provider, wireAPI))
		}
		if i.ProviderAuth {
			args = append(args, "--config", fmt.Sprintf("model_providers.%s.requires_openai_auth=true", provider))
		}
	}
	args = append(args, "exec")
	if strings.TrimSpace(i.ResumeSessionID) != "" {
		args = append(args, "resume")
	}
	// Never inherit the user's Codex config for an Adapter task. In particular,
	// config.toml may register global MCP servers; the task has no allowlist
	// unless this Adapter has a verified isolation boundary for it.
	args = append(args, "--ignore-user-config", "--json", "--output-last-message", i.OutputLastMessagePath)
	if i.Ephemeral {
		args = append(args, "--ephemeral")
	}
	if strings.TrimSpace(i.ResumeSessionID) != "" {
		args = append(args, i.ResumeSessionID)
	}
	args = append(args, prompt)
	return args, nil
}

type RawResult struct {
	ExitCode           int
	Stdout             []byte
	Stderr             []byte
	LastMessage        []byte
	LastMessageReadErr error
}

type Result struct {
	Status      DeliveryStatus
	Reason      string
	ExitCode    int
	FinalText   string
	Source      string
	Stdout      string
	Stderr      string
	LastMessage string
}

func (r RawResult) Normalize() Result {
	result := Result{
		ExitCode:    r.ExitCode,
		Stdout:      string(r.Stdout),
		Stderr:      string(r.Stderr),
		LastMessage: string(r.LastMessage),
	}
	if r.ExitCode != 0 {
		result.Status = StatusFailed
		result.Reason = "process_exit"
		return result
	}
	if r.LastMessageReadErr != nil {
		result.Status = StatusDeliveryMissing
		result.Reason = "last_message_read_failed"
		return result
	}
	if final := strings.TrimSpace(string(r.LastMessage)); final != "" {
		result.Status = StatusCompleted
		result.Reason = "durable_last_message"
		result.Source = "durable_last_message"
		result.FinalText = final
		return result
	}
	if final, source, parsedJSON := extractFinalText(r.Stdout); final != "" {
		result.Status = StatusCompleted
		result.Reason = "structured_final"
		result.Source = source
		result.FinalText = final
		return result
	} else if !parsedJSON {
		if plain := strings.TrimSpace(string(r.Stdout)); plain != "" {
			result.Status = StatusCompleted
			result.Reason = "stdout_final"
			result.Source = "stdout"
			result.FinalText = plain
			return result
		}
	}
	result.Status = StatusDeliveryMissing
	result.Reason = "delivery_missing"
	return result
}

func (i Invocation) Run(ctx context.Context, prompt string) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("context is required")
	}
	args, err := i.Args(prompt)
	if err != nil {
		return Result{}, err
	}
	if err := freshOutputLastMessagePath(i.OutputLastMessagePath); err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(ctx, i.Executable, args...)
	cmd.Dir = i.Workspace
	cmd.Env = append(os.Environ(), i.Environment...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	code := exitCode(err)
	result := RawResult{ExitCode: code, Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if raw, readErr := os.ReadFile(i.OutputLastMessagePath); readErr == nil {
		result.LastMessage = raw
	} else if !errors.Is(readErr, os.ErrNotExist) {
		result.LastMessageReadErr = readErr
	}
	if err != nil && code == -1 {
		return Result{}, fmt.Errorf("codex command failed to start: %w", err)
	}
	return result.Normalize(), nil
}

func freshOutputLastMessagePath(path string) error {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
		return errors.New("an absolute output-last-message path is required")
	}
	if _, err := os.Stat(path); err == nil {
		return errors.New("output-last-message path must not already exist")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect output-last-message path: %w", err)
	}
	return nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func extractFinalText(raw []byte) (text, source string, parsedJSON bool) {
	lines := bytes.Split(raw, []byte{'\n'})
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		parsedJSON = true
		if candidate, candidateSource := finalFromEvent(event); strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate), candidateSource, true
		}
	}
	return "", "", parsedJSON
}

func finalFromEvent(event map[string]any) (string, string) {
	eventType, _ := event["type"].(string)
	switch eventType {
	case "response.output_text.done", "assistant_message", "message.completed":
		if text, ok := event["text"].(string); ok {
			return text, eventType
		}
	case "item.completed":
		item, _ := event["item"].(map[string]any)
		itemType, _ := item["type"].(string)
		if itemType == "agent_message" {
			if text, ok := item["text"].(string); ok {
				return text, eventType
			}
		}
	case "turn.completed":
		if text, ok := event["last_message"].(string); ok {
			return text, eventType
		}
	}
	return "", ""
}

type PreflightRequest struct {
	Workspace       string
	Model           string
	Reasoning       string
	Permission      string
	Sandbox         string
	Approval        string
	TrustAuthorized bool
	MCPIsolation    string
	MCPAllowlist    []string
}

type PreflightResult struct {
	Status             string       `json:"status"`
	Code               string       `json:"code,omitempty"`
	Reason             string       `json:"reason,omitempty"`
	Version            string       `json:"version,omitempty"`
	ModelRegistered    bool         `json:"model_registered"`
	Capabilities       Capabilities `json:"capabilities"`
	CanonicalWorkspace string       `json:"canonical_workspace,omitempty"`
	MCPIsolation       string       `json:"mcp_isolation,omitempty"`
}

// Preflight performs only checks that are safe before an Attempt exists.
// Codex has no verified model-list command, so the explicitly registered
// model is preserved and the provider remains the authority for availability.
func (a *Adapter) Preflight(ctx context.Context, request PreflightRequest) (PreflightResult, error) {
	if ctx == nil {
		return PreflightResult{}, errors.New("context is required")
	}
	result := PreflightResult{Capabilities: a.Capabilities(), MCPIsolation: normalizedMCPIsolation(request.MCPIsolation)}
	workspace, err := orchestration.CanonicalDirectory(request.Workspace)
	if err != nil {
		return blocked(result, "workspace_invalid", err.Error()), nil
	}
	result.CanonicalWorkspace = workspace
	if strings.TrimSpace(request.Model) == "" {
		return blocked(result, "model_missing", "registered model is required"), nil
	}
	if err := validateProviderConfig(a.ModelProvider, a.ProviderBaseURL, a.ProviderWireAPI); err != nil {
		return blocked(result, "provider_config_invalid", err.Error()), nil
	}
	if _, err := normalizeCodexReasoning(request.Reasoning); err != nil {
		return unsupported(result, "reasoning_unsupported", err.Error()), nil
	}
	if normalizedPermission(request.Permission) != DefaultPermission {
		return unsupported(result, "write_capability_unproven", "only read-only Codex execution is admitted in Wave 3"), nil
	}
	if normalizedSandbox(request.Sandbox) != DefaultSandbox {
		return unsupported(result, "sandbox_write_unsupported", "only the explicit Codex read-only sandbox is admitted in Wave 3"), nil
	}
	if normalizedApproval(request.Approval) != DefaultApproval {
		return unsupported(result, "approval_policy_unsupported", "only the explicit untrusted approval policy is admitted in Wave 3"), nil
	}
	if !request.TrustAuthorized {
		return blocked(result, "workspace_trust_required", "workspace authorization requires an explicit caller decision before Codex launch"), nil
	}
	if issue := validateMCPRequest(result.MCPIsolation, request.MCPAllowlist); issue != nil {
		result.Status, result.Code, result.Reason = issue.Status, issue.Code, issue.Reason
		return result, nil
	}
	probe := a.Probe
	if probe == nil {
		probe = func(probeContext context.Context, executable string, args []string) (cli.Result, error) {
			return a.runner().Run(probeContext, cli.Command{Executable: executable, Args: args, Dir: workspace, Environment: append([]string(nil), a.Environment...)})
		}
	}
	versionResult, err := probe(ctx, a.executable(), []string{"--version"})
	if err != nil || versionResult.ExitCode != 0 {
		return blocked(result, "version_probe_failed", "Codex CLI version probe did not complete"), nil
	}
	result.Version = strings.TrimSpace(string(versionResult.Stdout))
	result.ModelRegistered = true
	result.Status = "passed"
	result.Reason = "explicit model binding admitted; provider validates model availability at launch"
	return result, nil
}

func blocked(result PreflightResult, code, reason string) PreflightResult {
	result.Status, result.Code, result.Reason = "blocked", code, reason
	return result
}

func unsupported(result PreflightResult, code, reason string) PreflightResult {
	result.Status, result.Code, result.Reason = string(CapabilityUnsupported), code, reason
	return result
}

type admissionIssue struct {
	Status string
	Code   string
	Reason string
}

func normalizedPermission(permission string) string {
	permission = strings.ToLower(strings.TrimSpace(permission))
	if permission == "" {
		return DefaultPermission
	}
	return permission
}

func normalizedSandbox(sandbox string) string {
	sandbox = strings.ToLower(strings.TrimSpace(sandbox))
	if sandbox == "" {
		return DefaultSandbox
	}
	return sandbox
}

func normalizedApproval(approval string) string {
	approval = strings.ToLower(strings.TrimSpace(approval))
	if approval == "" {
		return DefaultApproval
	}
	return approval
}

func normalizedMCPIsolation(isolation string) string {
	isolation = strings.ToLower(strings.TrimSpace(isolation))
	if isolation == "" {
		return "strict"
	}
	return isolation
}

func validateMCPRequest(isolation string, allowlist []string) *admissionIssue {
	isolation = normalizedMCPIsolation(isolation)
	if err := validateMCPAllowlist(allowlist); err != nil {
		return &admissionIssue{Status: "blocked", Code: "mcp_allowlist_invalid", Reason: err.Error()}
	}
	if isolation != "strict" && isolation != "unsupported" && isolation != "config-isolated" {
		return &admissionIssue{Status: string(CapabilityUnsupported), Code: "mcp_mode_unknown", Reason: "MCP isolation mode is not recognized"}
	}
	if len(allowlist) > 0 {
		return &admissionIssue{Status: string(CapabilityUnsupported), Code: "mcp_unsupported", Reason: "the verified Codex CLI path has no task/session MCP allowlist isolation boundary"}
	}
	if isolation == "config-isolated" {
		return &admissionIssue{Status: string(CapabilityUnsupported), Code: "mcp_config_isolation_unsupported", Reason: "the verified Codex CLI path has no adapter-owned isolated MCP config boundary"}
	}
	return nil
}

func validateMCPAllowlist(allowlist []string) error {
	seen := make(map[string]struct{}, len(allowlist))
	for _, server := range allowlist {
		server = strings.TrimSpace(server)
		if server == "" || server == "*" {
			return errors.New("MCP allowlist entries must be explicit non-wildcard names")
		}
		if _, exists := seen[server]; exists {
			return fmt.Errorf("MCP allowlist contains duplicate server %q", server)
		}
		seen[server] = struct{}{}
	}
	return nil
}

type LaunchRequest struct {
	Workspace             string
	Model                 string
	Reasoning             string
	Permission            string
	Sandbox               string
	Approval              string
	TrustAuthorized       bool
	MCPIsolation          string
	MCPAllowlist          []string
	Ephemeral             bool
	OutputLastMessagePath string
	Prompt                string
	TaskCanary            string
	TranscriptPath        string
}

type ExecutionResult struct {
	Status               string
	Reason               string
	ProviderHandle       string
	ProviderSessionID    string
	Terminal             bool
	ExecutionStarted     *bool
	SideEffectState      string
	Identity             map[string]string
	TaskCanary           string
	FinalText            string
	FinalSource          string
	RawEventRefs         []string
	TranscriptPath       string
	TranscriptWritten    bool
	LastMessagePath      string
	LastMessageAvailable bool
	ExitCode             int
	Stdout               []byte
	Stderr               []byte
}

func (r ExecutionResult) HostActionResult() orchestration.HostActionResult {
	return orchestration.HostActionResult{
		Status:            r.Status,
		Reason:            r.Reason,
		ProviderHandle:    r.ProviderHandle,
		ProviderSessionID: r.ProviderSessionID,
		Terminal:          r.Terminal,
		ExecutionStarted:  r.ExecutionStarted,
		SideEffectState:   r.SideEffectState,
		Identity:          copyMap(r.Identity),
		TaskCanary:        r.TaskCanary,
		RawEventRefs:      append([]string(nil), r.RawEventRefs...),
		FinalText:         r.FinalText,
	}
}

// Launch starts one Codex turn and waits only for the provider thread identity
// (or a terminal failure). Observe accepts terminal and final-delivery proof.
func (a *Adapter) Launch(ctx context.Context, request LaunchRequest) (ExecutionResult, error) {
	if ctx == nil {
		return ExecutionResult{}, errors.New("context is required")
	}
	normalized, err := normalizeLaunchRequest(request)
	if err != nil {
		return ExecutionResult{}, err
	}
	handleID := orchestration.NewID("codex-process")
	lastMessagePath, err := allocateLastMessagePath(normalized.Workspace, normalized.OutputLastMessagePath)
	if err != nil {
		return ExecutionResult{}, err
	}
	normalized.OutputLastMessagePath = lastMessagePath
	args, err := (Invocation{
		Executable:            a.executable(),
		Workspace:             normalized.Workspace,
		Model:                 normalized.Model,
		ModelProvider:         a.ModelProvider,
		ProviderBaseURL:       a.ProviderBaseURL,
		ProviderWireAPI:       a.ProviderWireAPI,
		ProviderAuth:          a.ProviderAuth,
		Reasoning:             normalized.Reasoning,
		Sandbox:               normalized.Sandbox,
		Approval:              normalized.Approval,
		OutputLastMessagePath: lastMessagePath,
		Ephemeral:             normalized.Ephemeral,
	}).Args(normalized.Prompt)
	if err != nil {
		_ = os.Remove(lastMessagePath)
		return ExecutionResult{}, err
	}
	turn, err := a.startTurn(ctx, normalized, args, "")
	if err != nil {
		_ = os.Remove(lastMessagePath)
		return ExecutionResult{}, err
	}
	s := &session{handleID: handleID, providerHandle: handleID, request: normalized, current: turn}
	a.mu.Lock()
	if a.sessions == nil {
		a.sessions = map[string]*session{}
	}
	a.sessions[handleID] = s
	a.mu.Unlock()
	go a.consumeTurn(s, turn)

	startupTimeout := a.StartupTimeout
	if startupTimeout <= 0 {
		startupTimeout = DefaultStartupTimeout
	}
	timer := time.NewTimer(startupTimeout)
	defer timer.Stop()
	select {
	case <-turn.ready:
		sid := turn.sessionID()
		s.setSessionID(sid)
		if sid != "" {
			a.mu.Lock()
			a.sessions[sid] = s
			a.mu.Unlock()
		}
		return s.runningResult(), nil
	case <-turn.done:
		return s.terminalResult(turn), nil
	case <-ctx.Done():
		cancel := turn.process.CancelWithCause(ctx.Err())
		return a.cancelledLaunchResult(s, turn, cancel, "launch_context_cancelled"), nil
	case <-timer.C:
		cancel := turn.process.CancelWithCause(context.DeadlineExceeded)
		return a.cancelledLaunchResult(s, turn, cancel, "startup_timeout"), nil
	}
}

func normalizeLaunchRequest(request LaunchRequest) (LaunchRequest, error) {
	workspace, err := orchestration.CanonicalDirectory(request.Workspace)
	if err != nil {
		return LaunchRequest{}, err
	}
	request.Workspace = workspace
	request.Model = strings.TrimSpace(request.Model)
	request.Permission = normalizedPermission(request.Permission)
	request.Sandbox = normalizedSandbox(request.Sandbox)
	request.Approval = normalizedApproval(request.Approval)
	request.MCPIsolation = normalizedMCPIsolation(request.MCPIsolation)
	if reasoning, err := normalizeCodexReasoning(request.Reasoning); err != nil {
		return LaunchRequest{}, err
	} else {
		request.Reasoning = reasoning
	}
	if request.Model == "" || strings.TrimSpace(request.Prompt) == "" || strings.TrimSpace(request.TaskCanary) == "" {
		return LaunchRequest{}, errors.New("model, prompt and task canary are required")
	}
	if request.Permission != DefaultPermission {
		return LaunchRequest{}, errors.New("Codex launch requires read-only permission in Wave 3")
	}
	if request.Sandbox != DefaultSandbox {
		return LaunchRequest{}, errors.New("Codex launch requires the explicit read-only sandbox in Wave 3")
	}
	if request.Approval != DefaultApproval {
		return LaunchRequest{}, errors.New("Codex launch requires the explicit untrusted approval policy in Wave 3")
	}
	if request.Ephemeral {
		return LaunchRequest{}, errors.New("Codex Adapter launch must retain a resumable session; ephemeral execution is only supported by direct Invocation")
	}
	if !request.TrustAuthorized {
		return LaunchRequest{}, errors.New("workspace authorization is required before Codex launch")
	}
	if issue := validateMCPRequest(request.MCPIsolation, request.MCPAllowlist); issue != nil {
		return LaunchRequest{}, errors.New(issue.Reason)
	}
	request.MCPAllowlist = append([]string(nil), request.MCPAllowlist...)
	if request.TranscriptPath != "" && !filepath.IsAbs(request.TranscriptPath) {
		return LaunchRequest{}, errors.New("transcript path must be absolute")
	}
	if request.OutputLastMessagePath != "" {
		if !filepath.IsAbs(request.OutputLastMessagePath) {
			return LaunchRequest{}, errors.New("output-last-message path must be absolute")
		}
		if err := ensureInside(request.Workspace, filepath.Clean(request.OutputLastMessagePath)); err != nil {
			return LaunchRequest{}, errors.New("output-last-message path must remain inside task workspace")
		}
	}
	return request, nil
}

type SendRequest struct {
	HandleID       string
	ActionID       string
	Prompt         string
	CoreAuthorized bool
	TaskCanary     string
}

// Send resumes only the verified Codex thread. The ActionID is rolled back on
// argument construction or process start failure so the core can safely retry
// a transport failure without duplicating a started turn.
func (a *Adapter) Send(ctx context.Context, request SendRequest) (ExecutionResult, error) {
	if ctx == nil {
		return ExecutionResult{}, errors.New("context is required")
	}
	if !request.CoreAuthorized || strings.TrimSpace(request.ActionID) == "" {
		return ExecutionResult{}, errors.New("send requires a core-authorized action")
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return ExecutionResult{}, errors.New("follow-up prompt is required")
	}
	s, err := a.findSession(request.HandleID)
	if err != nil {
		return ExecutionResult{}, err
	}
	s.mu.Lock()
	if s.current == nil || !s.current.isDone() {
		s.mu.Unlock()
		return ExecutionResult{}, errors.New("Codex session is still running")
	}
	if s.contaminated {
		s.mu.Unlock()
		return ExecutionResult{}, errors.New("Codex session is contaminated")
	}
	if s.sendInFlight {
		s.mu.Unlock()
		return ExecutionResult{}, errors.New("Codex session already has a follow-up in flight")
	}
	if s.providerSessionID == "" {
		s.mu.Unlock()
		return ExecutionResult{}, errors.New("Codex thread identity is missing")
	}
	if s.sendActions == nil {
		s.sendActions = map[string]struct{}{}
	}
	if _, alreadyUsed := s.sendActions[request.ActionID]; alreadyUsed {
		s.mu.Unlock()
		return ExecutionResult{}, errors.New("core send action was already used")
	}
	s.sendActions[request.ActionID] = struct{}{}
	if request.TaskCanary != "" && request.TaskCanary != s.request.TaskCanary {
		delete(s.sendActions, request.ActionID)
		s.mu.Unlock()
		return ExecutionResult{}, errors.New("task canary does not match the session")
	}
	s.sendInFlight = true
	base := s.request
	providerSessionID := s.providerSessionID
	s.mu.Unlock()

	lastMessagePath, err := allocateResumeLastMessagePath(base.OutputLastMessagePath)
	if err != nil {
		s.rollbackSend(request.ActionID)
		return ExecutionResult{}, err
	}
	base.OutputLastMessagePath = lastMessagePath
	args, err := (Invocation{
		Executable:            a.executable(),
		Workspace:             base.Workspace,
		Model:                 base.Model,
		ModelProvider:         a.ModelProvider,
		ProviderBaseURL:       a.ProviderBaseURL,
		ProviderWireAPI:       a.ProviderWireAPI,
		ProviderAuth:          a.ProviderAuth,
		Reasoning:             base.Reasoning,
		Sandbox:               base.Sandbox,
		Approval:              base.Approval,
		OutputLastMessagePath: lastMessagePath,
		ResumeSessionID:       providerSessionID,
	}).Args(request.Prompt)
	if err != nil {
		_ = os.Remove(lastMessagePath)
		s.rollbackSend(request.ActionID)
		return ExecutionResult{}, err
	}
	turn, err := a.startTurn(ctx, base, args, providerSessionID)
	if err != nil {
		_ = os.Remove(lastMessagePath)
		s.rollbackSend(request.ActionID)
		return ExecutionResult{}, err
	}
	s.mu.Lock()
	s.current = turn
	s.request.OutputLastMessagePath = lastMessagePath
	s.sendInFlight = false
	s.mu.Unlock()
	go a.consumeTurn(s, turn)
	return s.runningResult(), nil
}

func (s *session) rollbackSend(actionID string) {
	s.mu.Lock()
	delete(s.sendActions, actionID)
	s.sendInFlight = false
	s.mu.Unlock()
}

type ObserveRequest struct {
	HandleID string
}

func (a *Adapter) Observe(ctx context.Context, request ObserveRequest) (ExecutionResult, error) {
	if ctx == nil {
		return ExecutionResult{}, errors.New("context is required")
	}
	s, err := a.findSession(request.HandleID)
	if err != nil {
		return ExecutionResult{}, err
	}
	s.mu.Lock()
	turn := s.current
	s.mu.Unlock()
	if turn == nil {
		return ExecutionResult{}, errors.New("Codex session has no active turn")
	}
	if !turn.isDone() {
		return s.runningResult(), nil
	}
	return s.terminalResult(turn), nil
}

type CancelRequest struct {
	HandleID string
}

func (a *Adapter) Cancel(ctx context.Context, request CancelRequest) (ExecutionResult, error) {
	if ctx == nil {
		return ExecutionResult{}, errors.New("context is required")
	}
	s, err := a.findSession(request.HandleID)
	if err != nil {
		return ExecutionResult{}, err
	}
	s.mu.Lock()
	turn := s.current
	s.mu.Unlock()
	if turn == nil {
		return ExecutionResult{}, errors.New("Codex session has no active turn")
	}
	cancel := turn.process.Cancel()
	select {
	case <-turn.done:
		result := s.terminalResult(turn)
		if !cancel.Confirmed && result.Status == "cancelled" {
			result.Status, result.Reason = "cancel_unconfirmed", "process_kill_unconfirmed"
			result.SideEffectState = "unknown"
		}
		return result, nil
	case <-ctx.Done():
		return s.result("cancel_unconfirmed", "cancel_wait_expired", true), nil
	}
}

type CleanupRequest struct {
	HandleID string
}

func (a *Adapter) Cleanup(request CleanupRequest) error {
	s, err := a.findSession(request.HandleID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	turn := s.current
	providerSessionID := s.providerSessionID
	s.mu.Unlock()
	if turn != nil && !turn.isDone() {
		return errors.New("cannot cleanup a running Codex session")
	}
	a.mu.Lock()
	delete(a.sessions, s.handleID)
	delete(a.sessions, providerSessionID)
	a.mu.Unlock()
	return nil
}

func (a *Adapter) StageArtifact(workspace, path, format, content string, requiredSections []string) (orchestration.ArtifactRef, error) {
	return artifact.Stage(workspace, path, format, content, requiredSections)
}

type session struct {
	mu                sync.Mutex
	handleID          string
	providerHandle    string
	providerSessionID string
	request           LaunchRequest
	current           *turn
	sendActions       map[string]struct{}
	sendInFlight      bool
	contaminated      bool
}

type turn struct {
	process         *cli.Handle
	expectedSession string
	transcriptPath  string
	lastMessagePath string

	mu                 sync.Mutex
	summary            cli.StreamSummary
	result             cli.Result
	lastMessage        []byte
	lastMessageReadErr error
	transcriptWritten  bool
	done               chan struct{}
	ready              chan struct{}
	readyOnce          sync.Once
	doneOnce           sync.Once
}

func (a *Adapter) startTurn(ctx context.Context, request LaunchRequest, args []string, expectedSession string) (*turn, error) {
	process, err := a.runner().Start(ctx, cli.Command{Executable: a.executable(), Args: args, Dir: request.Workspace, Environment: append([]string(nil), a.Environment...)})
	if err != nil {
		return nil, err
	}
	turn := &turn{process: process, expectedSession: expectedSession, transcriptPath: request.TranscriptPath, lastMessagePath: request.OutputLastMessagePath, done: make(chan struct{}), ready: make(chan struct{})}
	if expectedSession != "" {
		turn.readyOnce.Do(func() { close(turn.ready) })
	}
	return turn, nil
}

func (a *Adapter) consumeTurn(s *session, turn *turn) {
	for {
		a.drainTurnOutput(turn)
		select {
		case <-turn.process.Finished():
			// Drain the runner's flushed final batch before reading durable
			// delivery and classifying the completed turn.
			a.drainTurnOutput(turn)
			processResult := turn.process.Wait()
			lastMessage, lastMessageReadErr := readLastMessage(turn.lastMessagePath)
			turn.mu.Lock()
			turn.result = processResult
			turn.lastMessage = append([]byte(nil), lastMessage...)
			turn.lastMessageReadErr = lastMessageReadErr
			if lastMessageReadErr == nil && strings.TrimSpace(string(lastMessage)) != "" {
				turn.summary.FinalText = strings.TrimSpace(string(lastMessage))
				turn.summary.FinalSource = "durable_last_message"
			}
			summaryRaw := append([]byte(nil), turn.process.Stdout()...)
			turn.mu.Unlock()
			if turn.transcriptPath != "" {
				writer := a.TranscriptWriter
				if writer == nil {
					writer = writeTranscript
				}
				transcriptRaw := append([]byte(nil), summaryRaw...)
				if len(lastMessage) > 0 {
					if len(transcriptRaw) > 0 && transcriptRaw[len(transcriptRaw)-1] != '\n' {
						transcriptRaw = append(transcriptRaw, '\n')
					}
					transcriptRaw = append(transcriptRaw, []byte("[durable_last_message]\n")...)
					transcriptRaw = append(transcriptRaw, lastMessage...)
				}
				transcriptErr := writer(turn.transcriptPath, transcriptRaw)
				turn.mu.Lock()
				turn.transcriptWritten = transcriptErr == nil
				turn.mu.Unlock()
			}
			turn.doneOnce.Do(func() { close(turn.done) })
			return
		default:
		}
		_ = turn.process.WaitForOutput(context.Background())
	}
}

func (a *Adapter) drainTurnOutput(turn *turn) {
	for _, line := range turn.process.DrainStdoutLines() {
		turn.mu.Lock()
		_ = turn.summary.AddLine(line)
		sessionID := turn.summary.SessionID
		turn.mu.Unlock()
		if sessionID != "" {
			turn.readyOnce.Do(func() { close(turn.ready) })
		}
	}
}

func readLastMessage(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("last-message path is missing")
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return raw, err
}

func (t *turn) isDone() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

func (t *turn) sessionID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.summary.SessionID
}

func (s *session) setSessionID(id string) {
	s.mu.Lock()
	s.providerSessionID = id
	s.mu.Unlock()
}

func (s *session) runningResult() ExecutionResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	started := true
	return ExecutionResult{Status: "running", Reason: "provider_thread_started", ProviderHandle: s.providerHandle, ProviderSessionID: s.providerSessionID, Terminal: false, ExecutionStarted: &started, SideEffectState: "active", Identity: identityMap(s), TaskCanary: s.request.TaskCanary, TranscriptPath: s.request.TranscriptPath}
}

func (s *session) terminalResult(turn *turn) ExecutionResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	turn.mu.Lock()
	summary := turn.summary
	processResult := turn.result
	lastMessageReadErr := turn.lastMessageReadErr
	transcriptWritten := turn.transcriptWritten
	lastMessagePath := turn.lastMessagePath
	turn.mu.Unlock()
	observedSessionID := summary.SessionID
	// A matching resume response may refresh local authority. A foreign
	// response is returned for Gateway contamination evidence but never becomes
	// the session ID used by a later Send.
	if observedSessionID != "" && (turn.expectedSession == "" || observedSessionID == turn.expectedSession) {
		s.providerSessionID = observedSessionID
	}
	if processResult.Cancelled {
		status, reason := "cancelled", "process_cancelled"
		if processResult.TimedOut {
			status, reason = "timed_out", "process_timeout"
		}
		return s.resultLocked(status, reason, true, summary, processResult, transcriptWritten, lastMessagePath, observedSessionID)
	}
	if processResult.ExitCode != 0 {
		return s.resultLocked("failed", "process_exit", true, summary, processResult, transcriptWritten, lastMessagePath, observedSessionID)
	}
	if lastMessageReadErr != nil {
		return s.resultLocked("failed", "last_message_read_failed", true, summary, processResult, transcriptWritten, lastMessagePath, observedSessionID)
	}
	if observedSessionID == "" {
		return s.resultLocked("failed", "thread_identity_missing", true, summary, processResult, transcriptWritten, lastMessagePath, "")
	}
	if turn.expectedSession != "" && observedSessionID != turn.expectedSession {
		s.contaminated = true
		return s.resultLocked("contaminated", "session_identity_mismatch", true, summary, processResult, transcriptWritten, lastMessagePath, observedSessionID)
	}
	if !summary.Terminal {
		return s.resultLocked("failed", "terminal_event_missing", true, summary, processResult, transcriptWritten, lastMessagePath, observedSessionID)
	}
	if providerTerminalError(summary.TerminalStatus) {
		return s.resultLocked("failed", "provider_terminal_error", true, summary, processResult, transcriptWritten, lastMessagePath, observedSessionID)
	}
	if !summary.DeliveryComplete() {
		return s.resultLocked("failed", "delivery_missing", true, summary, processResult, transcriptWritten, lastMessagePath, observedSessionID)
	}
	return s.resultLocked("completed", "terminal_and_final_delivery", true, summary, processResult, transcriptWritten, lastMessagePath, observedSessionID)
}

func providerTerminalError(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "error", "failed", "failure", "cancelled", "canceled", "aborted":
		return true
	default:
		return false
	}
}

func (s *session) result(status, reason string, terminal bool) ExecutionResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ExecutionResult{Status: status, Reason: reason, ProviderHandle: s.providerHandle, ProviderSessionID: s.providerSessionID, Terminal: terminal, ExecutionStarted: boolPtr(true), SideEffectState: "unknown", Identity: identityMap(s), TaskCanary: s.request.TaskCanary, TranscriptPath: s.request.TranscriptPath}
}

func (s *session) resultLocked(status, reason string, terminal bool, summary cli.StreamSummary, processResult cli.Result, transcriptWritten bool, lastMessagePath, observedSessionID string) ExecutionResult {
	refs := make([]string, 0, len(summary.Events)+2)
	for _, event := range summary.Events {
		refs = append(refs, fmt.Sprintf("stream:event:%d", event.Sequence))
	}
	if strings.TrimSpace(summary.FinalSource) == "durable_last_message" && lastMessagePath != "" {
		refs = append(refs, "durable_last_message:"+lastMessagePath)
	}
	if s.request.TranscriptPath != "" && transcriptWritten {
		refs = append(refs, "transcript:"+s.request.TranscriptPath)
	}
	identitySessionID := s.providerSessionID
	if strings.TrimSpace(observedSessionID) != "" {
		identitySessionID = observedSessionID
	}
	return ExecutionResult{Status: status, Reason: reason, ProviderHandle: s.providerHandle, ProviderSessionID: identitySessionID, Terminal: terminal, ExecutionStarted: boolPtr(true), SideEffectState: sideEffectFor(status), Identity: identityMapFor(s, identitySessionID), TaskCanary: s.request.TaskCanary, FinalText: summary.FinalText, FinalSource: summary.FinalSource, RawEventRefs: refs, TranscriptPath: s.request.TranscriptPath, TranscriptWritten: transcriptWritten, LastMessagePath: lastMessagePath, LastMessageAvailable: strings.TrimSpace(summary.FinalSource) == "durable_last_message", ExitCode: processResult.ExitCode, Stdout: append([]byte(nil), processResult.Stdout...), Stderr: append([]byte(nil), processResult.Stderr...)}
}

func (a *Adapter) cancelledLaunchResult(s *session, turn *turn, cancel cli.CancelResult, reason string) ExecutionResult {
	select {
	case <-turn.done:
		result := s.terminalResult(turn)
		if !cancel.Confirmed && result.Status == "cancelled" {
			result.Status, result.Reason = "cancel_unconfirmed", reason+"_cancel_unconfirmed"
			result.SideEffectState = "unknown"
		}
		return result
	case <-time.After(2 * time.Second):
		return s.result("cancel_unconfirmed", reason+"_cancel_unconfirmed", false)
	}
}

func identityMap(s *session) map[string]string {
	return identityMapFor(s, s.providerSessionID)
}

func identityMapFor(s *session, sessionID string) map[string]string {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	return map[string]string{"provider": Provider, "session": sessionID, "workspace": s.request.Workspace}
}

func sideEffectFor(status string) string {
	switch status {
	case "completed", "failed", "contaminated", "cancelled", "timed_out":
		return "none"
	default:
		return "unknown"
	}
}

func boolPtr(value bool) *bool { return &value }

func (a *Adapter) findSession(id string) (*session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("Codex session handle is required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[id]
	if !ok {
		for _, candidate := range a.sessions {
			candidate.mu.Lock()
			candidateSessionID := candidate.providerSessionID
			candidate.mu.Unlock()
			if candidateSessionID == id {
				s = candidate
				break
			}
		}
	}
	if s == nil {
		return nil, fmt.Errorf("unknown Codex session %q", id)
	}
	return s, nil
}

func (a *Adapter) runner() *cli.Runner {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.Runner == nil {
		a.Runner = cli.NewRunner()
	}
	return a.Runner
}

func (a *Adapter) executable() string {
	if strings.TrimSpace(a.Executable) == "" {
		return DefaultExecutable
	}
	return a.Executable
}

func allocateLastMessagePath(workspace, requested string) (string, error) {
	if strings.TrimSpace(requested) != "" {
		path := filepath.Clean(requested)
		if err := freshOutputLastMessagePath(path); err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", fmt.Errorf("create last-message directory: %w", err)
		}
		return path, nil
	}
	directory := filepath.Join(workspace, ".agent-workflow", Provider, "delivery")
	return reserveTemporaryPath(directory, ".last-message-*")
}

func allocateResumeLastMessagePath(basePath string) (string, error) {
	if strings.TrimSpace(basePath) == "" || !filepath.IsAbs(basePath) {
		return "", errors.New("a launch last-message path is required before resume")
	}
	return reserveTemporaryPath(filepath.Dir(basePath), "."+filepath.Base(basePath)+".resume-*")
}

func reserveTemporaryPath(directory, pattern string) (string, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create last-message directory: %w", err)
	}
	file, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return "", fmt.Errorf("reserve last-message path: %w", err)
	}
	path := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("set last-message permissions: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close reserved last-message path: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("release reserved last-message path: %w", err)
	}
	return path, nil
}

func ensureInside(workspace, path string) error {
	relative, err := filepath.Rel(workspace, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("path is outside task workspace")
	}
	return nil
}

func copyMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func writeTranscript(path string, raw []byte) error {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
		return errors.New("transcript path must be absolute")
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(parent, ".codex-transcript-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err == nil {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	backup := path + ".previous"
	_ = os.Remove(backup)
	if err := os.Rename(path, backup); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Rename(backup, path)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

// ExtractIdentity verifies the minimum provider/session/workspace/canary
// evidence required by the core before an external result is accepted.
func ExtractIdentity(providerSessionID, workspace, expectedAdapter, expectedSession string, taskCanary, returnedCanary string) (map[string]string, error) {
	if strings.TrimSpace(providerSessionID) == "" || strings.TrimSpace(workspace) == "" {
		return nil, errors.New("provider session and workspace identity are required")
	}
	if expectedSession != "" && providerSessionID != expectedSession {
		return nil, errors.New("provider session identity mismatch")
	}
	if expectedAdapter != "" && !strings.EqualFold(strings.TrimSpace(expectedAdapter), Provider) {
		return nil, errors.New("provider identity does not match Codex adapter")
	}
	canonical, err := orchestration.CanonicalDirectory(workspace)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(taskCanary) == "" || taskCanary != returnedCanary {
		return nil, errors.New("task canary is missing or mismatched")
	}
	return map[string]string{"provider": Provider, "session": providerSessionID, "workspace": canonical}, nil
}
