// Package claude implements the external Claude Code CLI boundary.
//
// The Wave 3 Claude path is deliberately admitted only for explicit,
// read-only plan execution. It keeps the provider session and JSONL stream
// separate from the core's Attempt/Completion state, and never treats an exit
// code alone as final delivery evidence.
package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"agent-workflow-toolkit/internal/adapters/artifact"
	"agent-workflow-toolkit/internal/adapters/cli"
	"agent-workflow-toolkit/internal/orchestration"
)

const (
	Provider              = "claude"
	DefaultExecutable     = "claude"
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
	sandbox := Capability{Status: CapabilityUnsupported, Reason: "the verified Claude CLI contract exposes permission modes, not a task sandbox boundary"}
	if runtime.GOOS != "windows" {
		sandbox = Capability{Status: CapabilityDegraded, Reason: "Claude process cancellation is verified; provider sandbox semantics remain platform-specific"}
	}
	return Capabilities{
		Launch:         Capability{Status: CapabilitySupported, Reason: "argv-only process launch with stream-json capture"},
		Observe:        Capability{Status: CapabilitySupported, Reason: "retained process/session stream observation"},
		Send:           Capability{Status: CapabilitySupported, Reason: "resume requires a core-authorized action and verified session identity"},
		Cancel:         Capability{Status: CapabilitySupported, Reason: "process-tree cancellation with explicit unconfirmed state"},
		Cleanup:        Capability{Status: CapabilitySupported, Reason: "session reference cleanup after terminal process"},
		WorkspaceTrust: Capability{Status: CapabilitySupported, Reason: "launch requires explicit caller authorization because print mode skips the trust dialog"},
		Permission:     Capability{Status: CapabilitySupported, Reason: "read-only maps to Claude permission-mode plan"},
		Sandbox:        sandbox,
		MCP:            Capability{Status: CapabilitySupported, Reason: "strict task MCP configuration is passed with --strict-mcp-config"},
		ModelProbe:     Capability{Status: CapabilityDegraded, Reason: "Claude CLI exposes no verified preflight model-list command; the explicit binding is used without fallback"},
		Artifact:       Capability{Status: CapabilitySupported, Reason: "core-assigned workspace artifact staging"},
	}
}

type ProbeFunc func(context.Context, string, []string) (cli.Result, error)

// Adapter owns external Claude processes and their session evidence. Runner
// and Probe are injectable for deterministic lifecycle and preflight tests.
type Adapter struct {
	Executable       string
	Environment      []string
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

type PreflightRequest struct {
	Workspace       string
	Model           string
	Reasoning       string
	Permission      string
	TrustAuthorized bool
	MCPIsolation    string
	MCPAllowlist    []string
	MCPConfigPath   string
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

// Preflight performs checks that can be completed before an Attempt is
// created. Claude has no reliable model-list command, so the model is treated
// as the explicitly registered binding and is never silently replaced or
// given a fallback model.
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
	if permission := normalizedPermission(request.Permission); permission != DefaultPermission {
		return unsupported(result, "write_capability_unproven", "only read-only Claude execution is admitted in Wave 3"), nil
	}
	if _, err := normalizedReasoning(request.Reasoning); err != nil {
		return unsupported(result, "reasoning_unsupported", err.Error()), nil
	}
	if !request.TrustAuthorized {
		return blocked(result, "workspace_trust_required", "workspace trust requires explicit user authorization before print-mode launch"), nil
	}
	if issue := validateMCPRequest(workspace, result.MCPIsolation, request.MCPConfigPath, request.MCPAllowlist); issue != nil {
		result.Status, result.Code, result.Reason = issue.Status, issue.Code, issue.Reason
		return result, nil
	}
	probe := a.Probe
	if probe == nil {
		if a.Runner == nil {
			a.Runner = cli.NewRunner()
		}
		probe = func(probeContext context.Context, executable string, args []string) (cli.Result, error) {
			return a.Runner.Run(probeContext, cli.Command{Executable: executable, Args: args, Dir: workspace, Environment: append([]string(nil), a.Environment...)})
		}
	}
	versionResult, err := probe(ctx, a.executable(), []string{"--version"})
	if err != nil || versionResult.ExitCode != 0 {
		return blocked(result, "version_probe_failed", "Claude Code version probe did not complete"), nil
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

func normalizedMCPIsolation(isolation string) string {
	isolation = strings.ToLower(strings.TrimSpace(isolation))
	if isolation == "" {
		return "strict"
	}
	return isolation
}

func normalizedReasoning(reasoning string) (string, error) {
	reasoning = strings.ToLower(strings.TrimSpace(reasoning))
	if reasoning == "" || reasoning == "none" {
		return "", nil
	}
	switch reasoning {
	case "low", "medium", "high", "xhigh", "max":
		return reasoning, nil
	default:
		return "", fmt.Errorf("Claude effort level %q is not supported by the verified CLI contract", reasoning)
	}
}

func validateMCPRequest(workspace, isolation, configPath string, allowlist []string) *admissionIssue {
	isolation = normalizedMCPIsolation(isolation)
	switch isolation {
	case "strict", "config-isolated":
	case "unsupported":
		if len(allowlist) > 0 || strings.TrimSpace(configPath) != "" {
			return &admissionIssue{Status: string(CapabilityUnsupported), Code: "mcp_unsupported", Reason: "the requested task MCP configuration is disabled for this execution path"}
		}
	default:
		return &admissionIssue{Status: string(CapabilityUnsupported), Code: "mcp_mode_unknown", Reason: "MCP isolation mode is not recognized"}
	}
	if err := validateMCPAllowlist(allowlist); err != nil {
		return &admissionIssue{Status: "blocked", Code: "mcp_allowlist_invalid", Reason: err.Error()}
	}
	if strings.TrimSpace(configPath) == "" {
		if len(allowlist) > 0 {
			return &admissionIssue{Status: "blocked", Code: "mcp_config_required", Reason: "an explicit task MCP config is required for the requested allowlist"}
		}
		return nil
	}
	if len(allowlist) == 0 {
		return &admissionIssue{Status: "blocked", Code: "mcp_allowlist_required", Reason: "an MCP config cannot be admitted without an explicit non-wildcard allowlist"}
	}
	if err := validateMCPConfig(workspace, configPath, allowlist); err != nil {
		return &admissionIssue{Status: "blocked", Code: "mcp_config_invalid", Reason: err.Error()}
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

func validateMCPConfig(workspace, configPath string, allowlist []string) error {
	if !filepath.IsAbs(configPath) {
		return errors.New("MCP config path must be absolute")
	}
	path := filepath.Clean(configPath)
	if err := ensureInside(workspace, path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("MCP config is not accessible: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("MCP config must be a regular file")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve MCP config: %w", err)
	}
	if err := ensureInside(workspace, filepath.Clean(resolved)); err != nil {
		return errors.New("MCP config resolves outside task workspace")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read MCP config: %w", err)
	}
	var root struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("parse MCP config: %w", err)
	}
	if len(root.MCPServers) != len(allowlist) {
		return errors.New("MCP config servers do not exactly match the requested allowlist")
	}
	for _, server := range allowlist {
		if _, ok := root.MCPServers[server]; !ok {
			return fmt.Errorf("MCP config is missing requested server %q", server)
		}
	}
	return nil
}

type Invocation struct {
	Executable      string
	Workspace       string
	Model           string
	Reasoning       string
	Permission      string
	Trust           bool
	MCPIsolation    string
	MCPAllowlist    []string
	MCPConfigPath   string
	ResumeSessionID string
	DisableTools    bool
}

func (i Invocation) Args(prompt string) ([]string, error) {
	if strings.TrimSpace(i.Executable) == "" || strings.TrimSpace(i.Workspace) == "" || !filepath.IsAbs(i.Workspace) {
		return nil, errors.New("Claude executable and absolute workspace are required")
	}
	if strings.TrimSpace(i.Model) == "" {
		return nil, errors.New("registered Claude model is required")
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("prompt is required")
	}
	if normalizedPermission(i.Permission) != DefaultPermission {
		return nil, errors.New("Claude invocation requires read-only permission in Wave 3")
	}
	if !i.Trust {
		return nil, errors.New("workspace trust authorization is required before Claude launch")
	}
	reasoning, err := normalizedReasoning(i.Reasoning)
	if err != nil {
		return nil, err
	}
	if issue := validateMCPRequest(i.Workspace, i.MCPIsolation, i.MCPConfigPath, i.MCPAllowlist); issue != nil {
		return nil, errors.New(issue.Reason)
	}
	args := []string{
		"--model", i.Model,
		"--output-format", "stream-json",
		"--verbose",
		"--permission-mode", "plan",
		"--strict-mcp-config",
	}
	if reasoning != "" {
		args = append(args, "--effort", reasoning)
	}
	if i.DisableTools {
		args = append(args, "--tools=")
	}
	if strings.TrimSpace(i.MCPConfigPath) != "" {
		args = append(args, "--mcp-config", filepath.Clean(i.MCPConfigPath))
	}
	if strings.TrimSpace(i.ResumeSessionID) != "" {
		args = append(args, "--resume", i.ResumeSessionID)
	}
	args = append(args, "-p", prompt)
	return args, nil
}

type LaunchRequest struct {
	Workspace       string
	Model           string
	Reasoning       string
	Permission      string
	TrustAuthorized bool
	MCPIsolation    string
	MCPAllowlist    []string
	MCPConfigPath   string
	DisableTools    bool
	Prompt          string
	TaskCanary      string
	TranscriptPath  string
}

type ExecutionResult struct {
	Status            string
	Reason            string
	ProviderHandle    string
	ProviderSessionID string
	Terminal          bool
	ExecutionStarted  *bool
	SideEffectState   string
	Identity          map[string]string
	TaskCanary        string
	FinalText         string
	FinalSource       string
	RawEventRefs      []string
	TranscriptPath    string
	TranscriptWritten bool
	ExitCode          int
	Stdout            []byte
	Stderr            []byte
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

// Launch starts one Claude turn and waits only for the provider session
// identity (or a terminal failure). The core must observe the returned handle
// to accept terminal and final-delivery evidence.
func (a *Adapter) Launch(ctx context.Context, request LaunchRequest) (ExecutionResult, error) {
	if ctx == nil {
		return ExecutionResult{}, errors.New("context is required")
	}
	normalized, err := normalizeLaunchRequest(request)
	if err != nil {
		return ExecutionResult{}, err
	}
	args, err := (Invocation{
		Executable: a.executable(), Workspace: normalized.Workspace, Model: normalized.Model,
		Reasoning: normalized.Reasoning, Permission: normalized.Permission, Trust: normalized.TrustAuthorized,
		MCPIsolation: normalized.MCPIsolation, MCPAllowlist: normalized.MCPAllowlist, MCPConfigPath: normalized.MCPConfigPath,
		DisableTools: normalized.DisableTools,
	}).Args(normalized.Prompt)
	if err != nil {
		return ExecutionResult{}, err
	}
	turn, err := a.startTurn(ctx, normalized, args, "")
	if err != nil {
		return ExecutionResult{}, err
	}
	handleID := orchestration.NewID("claude-process")
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
		turn.process.Cancel()
		select {
		case <-turn.done:
			return s.terminalResult(turn), nil
		case <-time.After(2 * time.Second):
			return s.result("cancel_unconfirmed", "launch_context_cancelled", false), nil
		}
	case <-timer.C:
		turn.process.CancelWithCause(context.DeadlineExceeded)
		select {
		case <-turn.done:
			return s.terminalResult(turn), nil
		case <-time.After(2 * time.Second):
			return s.result("cancel_unconfirmed", "startup_timeout_cancel_unconfirmed", false), nil
		}
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
	request.MCPIsolation = normalizedMCPIsolation(request.MCPIsolation)
	if normalized, err := normalizedReasoning(request.Reasoning); err != nil {
		return LaunchRequest{}, err
	} else {
		request.Reasoning = normalized
	}
	if request.Model == "" || strings.TrimSpace(request.Prompt) == "" || strings.TrimSpace(request.TaskCanary) == "" {
		return LaunchRequest{}, errors.New("model, prompt and task canary are required")
	}
	if request.Permission != DefaultPermission {
		return LaunchRequest{}, errors.New("Claude launch requires read-only permission in Wave 3")
	}
	if !request.TrustAuthorized {
		return LaunchRequest{}, errors.New("workspace trust authorization is required before Claude launch")
	}
	if issue := validateMCPRequest(request.Workspace, request.MCPIsolation, request.MCPConfigPath, request.MCPAllowlist); issue != nil {
		return LaunchRequest{}, errors.New(issue.Reason)
	}
	if request.TranscriptPath != "" && !filepath.IsAbs(request.TranscriptPath) {
		return LaunchRequest{}, errors.New("transcript path must be absolute")
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

// Send requires a core-issued action and resumes only the already verified
// provider session. Failed invocation/start attempts release the action ID so
// the core may retry the same action without duplicating a successful turn.
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
		return ExecutionResult{}, errors.New("Claude session is still running")
	}
	if s.contaminated {
		s.mu.Unlock()
		return ExecutionResult{}, errors.New("Claude session is contaminated")
	}
	if s.sendInFlight {
		s.mu.Unlock()
		return ExecutionResult{}, errors.New("Claude session already has a follow-up in flight")
	}
	if s.providerSessionID == "" {
		s.mu.Unlock()
		return ExecutionResult{}, errors.New("Claude session identity is missing")
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
	args, err := (Invocation{
		Executable: a.executable(), Workspace: base.Workspace, Model: base.Model, Reasoning: base.Reasoning,
		Permission: base.Permission, Trust: base.TrustAuthorized, MCPIsolation: base.MCPIsolation,
		MCPAllowlist: base.MCPAllowlist, MCPConfigPath: base.MCPConfigPath, ResumeSessionID: providerSessionID,
		DisableTools: base.DisableTools,
	}).Args(request.Prompt)
	if err != nil {
		s.rollbackSend(request.ActionID)
		return ExecutionResult{}, err
	}
	turn, err := a.startTurn(ctx, base, args, providerSessionID)
	if err != nil {
		s.rollbackSend(request.ActionID)
		return ExecutionResult{}, err
	}
	s.mu.Lock()
	s.current = turn
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
		return ExecutionResult{}, errors.New("Claude session has no active turn")
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
		return ExecutionResult{}, errors.New("Claude session has no active turn")
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
		return errors.New("cannot cleanup a running Claude session")
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

	mu                sync.Mutex
	summary           cli.StreamSummary
	result            cli.Result
	transcriptWritten bool
	done              chan struct{}
	ready             chan struct{}
	readyOnce         sync.Once
	doneOnce          sync.Once
}

func (a *Adapter) startTurn(ctx context.Context, request LaunchRequest, args []string, expectedSession string) (*turn, error) {
	if a.Runner == nil {
		a.Runner = cli.NewRunner()
	}
	process, err := a.Runner.Start(ctx, cli.Command{Executable: a.executable(), Args: args, Dir: request.Workspace, Environment: append([]string(nil), a.Environment...)})
	if err != nil {
		return nil, err
	}
	turn := &turn{process: process, expectedSession: expectedSession, transcriptPath: request.TranscriptPath, done: make(chan struct{}), ready: make(chan struct{})}
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
			// The runner flushes collector lines before closing Finished; consume
			// that final batch before terminal/final delivery classification.
			a.drainTurnOutput(turn)
			processResult := turn.process.Wait()
			turn.mu.Lock()
			turn.result = processResult
			turn.mu.Unlock()
			if turn.transcriptPath != "" {
				writer := a.TranscriptWriter
				if writer == nil {
					writer = writeTranscript
				}
				transcriptErr := writer(turn.transcriptPath, turn.process.Stdout())
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
	return ExecutionResult{Status: "running", Reason: "provider_session_started", ProviderHandle: s.providerHandle, ProviderSessionID: s.providerSessionID, Terminal: false, ExecutionStarted: &started, SideEffectState: "active", Identity: identityMap(s), TaskCanary: s.request.TaskCanary, TranscriptPath: s.request.TranscriptPath}
}

func (s *session) terminalResult(turn *turn) ExecutionResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	turn.mu.Lock()
	summary := turn.summary
	processResult := turn.result
	transcriptWritten := turn.transcriptWritten
	turn.mu.Unlock()
	// The provider session ID is local resume authority. Only an initial or
	// expected matching response may update it; a mismatching resume response
	// must remain observable in the returned evidence without becoming the ID
	// used by a later Send.
	if summary.SessionID != "" && (turn.expectedSession == "" || summary.SessionID == turn.expectedSession) {
		s.providerSessionID = summary.SessionID
	}
	if processResult.Cancelled {
		status, reason := "cancelled", "process_cancelled"
		if processResult.TimedOut {
			status, reason = "timed_out", "process_timeout"
		}
		return s.resultLocked(status, reason, true, summary, processResult, transcriptWritten, summary.SessionID)
	}
	if processResult.ExitCode != 0 {
		return s.resultLocked("failed", "process_exit", true, summary, processResult, transcriptWritten, summary.SessionID)
	}
	if summary.SessionID == "" {
		return s.resultLocked("failed", "session_identity_missing", true, summary, processResult, transcriptWritten, "")
	}
	if turn.expectedSession != "" && summary.SessionID != turn.expectedSession {
		s.contaminated = true
		return s.resultLocked("contaminated", "session_identity_mismatch", true, summary, processResult, transcriptWritten, summary.SessionID)
	}
	if !summary.Terminal {
		return s.resultLocked("failed", "terminal_event_missing", true, summary, processResult, transcriptWritten, summary.SessionID)
	}
	if providerTerminalError(summary.TerminalStatus) {
		return s.resultLocked("failed", "provider_terminal_error", true, summary, processResult, transcriptWritten, summary.SessionID)
	}
	if !summary.DeliveryComplete() {
		return s.resultLocked("failed", "delivery_missing", true, summary, processResult, transcriptWritten, summary.SessionID)
	}
	return s.resultLocked("completed", "terminal_and_final_delivery", true, summary, processResult, transcriptWritten, summary.SessionID)
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

func (s *session) resultLocked(status, reason string, terminal bool, summary cli.StreamSummary, processResult cli.Result, transcriptWritten bool, observedSessionID string) ExecutionResult {
	refs := make([]string, 0, len(summary.Events)+1)
	for _, event := range summary.Events {
		refs = append(refs, fmt.Sprintf("stream:event:%d", event.Sequence))
	}
	if s.request.TranscriptPath != "" && transcriptWritten {
		refs = append(refs, "transcript:"+s.request.TranscriptPath)
	}
	identitySessionID := s.providerSessionID
	if strings.TrimSpace(observedSessionID) != "" {
		identitySessionID = observedSessionID
	}
	return ExecutionResult{Status: status, Reason: reason, ProviderHandle: s.providerHandle, ProviderSessionID: identitySessionID, Terminal: terminal, ExecutionStarted: boolPtr(true), SideEffectState: sideEffectFor(status), Identity: identityMapFor(s, identitySessionID), TaskCanary: s.request.TaskCanary, FinalText: summary.FinalText, FinalSource: summary.FinalSource, RawEventRefs: refs, TranscriptPath: s.request.TranscriptPath, TranscriptWritten: transcriptWritten, ExitCode: processResult.ExitCode, Stdout: append([]byte(nil), processResult.Stdout...), Stderr: append([]byte(nil), processResult.Stderr...)}
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
		return nil, errors.New("Claude session handle is required")
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
		return nil, fmt.Errorf("unknown Claude session %q", id)
	}
	return s, nil
}

func (a *Adapter) executable() string {
	if strings.TrimSpace(a.Executable) == "" {
		return DefaultExecutable
	}
	return a.Executable
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
	temporary, err := os.CreateTemp(parent, ".claude-transcript-*")
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

func ensureInside(workspace, path string) error {
	relative, err := filepath.Rel(workspace, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("MCP config path is outside task workspace")
	}
	return nil
}

// ExtractIdentity verifies the minimum identity/canary evidence required by
// the core before an external result can be accepted.
func ExtractIdentity(providerSessionID, workspace, expectedAdapter, expectedSession string, taskCanary, returnedCanary string) (map[string]string, error) {
	if strings.TrimSpace(providerSessionID) == "" || strings.TrimSpace(workspace) == "" {
		return nil, errors.New("provider session and workspace identity are required")
	}
	if expectedSession != "" && providerSessionID != expectedSession {
		return nil, errors.New("provider session identity mismatch")
	}
	if expectedAdapter != "" && !strings.EqualFold(strings.TrimSpace(expectedAdapter), Provider) {
		return nil, errors.New("provider identity does not match Claude adapter")
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
