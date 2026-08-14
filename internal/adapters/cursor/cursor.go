// Package cursor implements the external Cursor Agent CLI boundary.
//
// The adapter is intentionally read-only on Windows for Wave 3. Workspace
// trust, process permissions, MCP isolation and final delivery are separate
// capability decisions; a successful CLI exit is not completion evidence.
package cursor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	Provider              = "cursor"
	DefaultExecutable     = "agent"
	DefaultMode           = "plan"
	DefaultStartupTimeout = 2 * time.Minute
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
	Sandbox        Capability `json:"sandbox"`
	MCP            Capability `json:"mcp"`
	Artifact       Capability `json:"artifact"`
}

func (a *Adapter) Capabilities() Capabilities {
	sandbox := Capability{Status: CapabilityDegraded, Reason: "Cursor sandbox capability is not assumed without a platform-specific probe"}
	if runtime.GOOS == "windows" {
		sandbox = Capability{Status: CapabilityUnsupported, Reason: "Cursor sandbox enabled mode is unavailable on Windows"}
	}
	return Capabilities{
		Launch:         Capability{Status: CapabilitySupported, Reason: "argv-only process launch with stream-json capture"},
		Observe:        Capability{Status: CapabilitySupported, Reason: "retained process/session stream observation"},
		Send:           Capability{Status: CapabilitySupported, Reason: "resume requires a core-authorized action"},
		Cancel:         Capability{Status: CapabilitySupported, Reason: "process-tree cancellation with unconfirmed state"},
		Cleanup:        Capability{Status: CapabilitySupported, Reason: "session reference cleanup after terminal process"},
		WorkspaceTrust: Capability{Status: CapabilitySupported, Reason: "explicit --trust only after user authorization"},
		Sandbox:        sandbox,
		MCP:            Capability{Status: CapabilityUnsupported, Reason: "task/session MCP allowlist isolation is not exposed by the verified CLI contract"},
		Artifact:       Capability{Status: CapabilitySupported, Reason: "core-assigned workspace artifact staging"},
	}
}

type ProbeFunc func(context.Context, string, []string) (cli.Result, error)

// Adapter owns external Cursor processes and their session evidence. Runner
// is injectable only to make process lifecycle tests deterministic; production
// construction uses the standard argv-only runner.
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
	Mode            string
	Permission      string
	TrustAuthorized bool
	Sandbox         string
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
}

// Preflight performs configuration and executable/model checks before an
// Attempt is created. Blocked/unsupported results are policy results, not Go
// errors, so callers can persist them without manufacturing a provider run.
func (a *Adapter) Preflight(ctx context.Context, request PreflightRequest) (PreflightResult, error) {
	result := PreflightResult{Capabilities: a.Capabilities()}
	workspace, err := orchestration.CanonicalDirectory(request.Workspace)
	if err != nil {
		return blocked(result, "workspace_invalid", err.Error()), nil
	}
	result.CanonicalWorkspace = workspace
	if strings.TrimSpace(request.Model) == "" {
		return blocked(result, "model_missing", "registered model is required"), nil
	}
	mode := normalizedMode(request.Mode)
	permission := normalizedPermission(request.Permission)
	if mode != "plan" && mode != "ask" {
		return unsupported(result, "mode_write_capability_unproven", "only plan and ask read-only modes are admitted in Wave 3"), nil
	}
	if permission != "read-only" {
		return unsupported(result, "windows_write_capability_unproven", "Cursor write execution is not admitted without a verified allowlist boundary"), nil
	}
	if !request.TrustAuthorized {
		return blocked(result, "workspace_trust_required", "workspace trust requires explicit user authorization"), nil
	}
	if sandbox := strings.ToLower(strings.TrimSpace(request.Sandbox)); sandbox != "" && sandbox != "disabled" {
		return unsupported(result, "windows_sandbox_unsupported", "the adapter does not pass Cursor sandbox enabled mode on this platform"), nil
	}
	if len(request.MCPAllowlist) > 0 {
		switch strings.ToLower(strings.TrimSpace(request.MCPIsolation)) {
		case "strict":
			return unsupported(result, "mcp_strict_unsupported", "Cursor CLI has no verified task/session MCP allowlist boundary"), nil
		case "config-isolated":
			return unsupported(result, "mcp_config_isolation_unsupported", "an external Cursor config boundary was not verified by this adapter"), nil
		case "unsupported", "":
			return unsupported(result, "mcp_unsupported", "the requested task MCP allowlist cannot be isolated"), nil
		default:
			return unsupported(result, "mcp_mode_unknown", "MCP isolation mode is not recognized"), nil
		}
	}
	if ctx == nil {
		return PreflightResult{}, errors.New("context is required")
	}
	// A real Windows preflight must prove that Launch can bypass the shell
	// wrappers which flatten multiline Prompt argv. Unit tests may inject Probe
	// and exercise policy branches without requiring a local Cursor install.
	if runtime.GOOS == "windows" && a.Probe == nil {
		if _, _, nativeErr := resolveWindowsCursorNative(a.executable()); nativeErr != nil {
			return blocked(result, "native_prompt_transport_unavailable", nativeErr.Error()), nil
		}
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
		return blocked(result, "version_probe_failed", "Cursor Agent version probe did not complete"), nil
	}
	result.Version = strings.TrimSpace(string(versionResult.Stdout))
	modelsResult, err := probe(ctx, a.executable(), []string{"models"})
	if err != nil || modelsResult.ExitCode != 0 {
		return blocked(result, "model_probe_failed", "Cursor Agent model probe did not complete"), nil
	}
	if !modelListed(string(modelsResult.Stdout), request.Model) {
		return blocked(result, "model_not_registered", "requested model was not listed by Cursor Agent"), nil
	}
	result.ModelRegistered = true
	result.Status = "passed"
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

func degraded(result PreflightResult, code, reason string) PreflightResult {
	result.Status, result.Code, result.Reason = string(CapabilityDegraded), code, reason
	return result
}

func modelListed(raw, model string) bool {
	model = strings.TrimSpace(model)
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.TrimSpace(fields[0]) == model {
			return true
		}
	}
	return false
}

func normalizedMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return DefaultMode
	}
	return mode
}

func normalizedPermission(permission string) string {
	permission = strings.ToLower(strings.TrimSpace(permission))
	if permission == "" {
		return "read-only"
	}
	return permission
}

type Invocation struct {
	Executable         string
	Workspace          string
	Model              string
	Mode               string
	Trust              bool
	ResumeSessionID    string
	ContinueLatest     bool
	Environment        []string
	AdditionalReadDirs []string
}

func (i Invocation) Args(prompt string) ([]string, error) {
	if strings.TrimSpace(i.Executable) == "" || strings.TrimSpace(i.Workspace) == "" || !filepath.IsAbs(i.Workspace) {
		return nil, errors.New("Cursor executable and absolute workspace are required")
	}
	if strings.TrimSpace(i.Model) == "" {
		return nil, errors.New("registered Cursor model is required")
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("prompt is required")
	}
	mode := normalizedMode(i.Mode)
	if mode != "plan" && mode != "ask" {
		return nil, fmt.Errorf("unsupported Cursor mode %q", mode)
	}
	if i.ResumeSessionID != "" && i.ContinueLatest {
		return nil, errors.New("resume session and continue-latest are mutually exclusive")
	}
	args := []string{"--print", "--output-format", "stream-json", "--mode", mode}
	if i.Trust {
		args = append(args, "--trust")
	}
	args = append(args, "--workspace", i.Workspace, "--model", i.Model)
	for _, directory := range i.AdditionalReadDirs {
		if strings.TrimSpace(directory) == "" || !filepath.IsAbs(directory) {
			return nil, errors.New("Cursor additional read directories must be absolute")
		}
		args = append(args, "--add-dir", filepath.Clean(directory))
	}
	if strings.TrimSpace(i.ResumeSessionID) != "" {
		args = append(args, "--resume", i.ResumeSessionID)
	} else if i.ContinueLatest {
		args = append(args, "--continue")
	}
	// The Prompt is one exact positional argument. Cursor concatenates multiple
	// prompt positionals with spaces, so splitting here would destroy blank
	// lines, indentation, Markdown and code blocks. Command resolves the native
	// Windows launcher before this argv is executed.
	args = append(args, "--", prompt)
	return args, nil
}

// Command resolves the shell-free process entry used for one Cursor turn.
// The installed Windows agent.cmd/agent.ps1 wrappers re-serialize argv and do
// not preserve a multiline Prompt. The distribution's native node.exe plus
// index.js path does preserve one multiline argument exactly.
func (i Invocation) Command(prompt string) (string, []string, error) {
	args, err := i.Args(prompt)
	if err != nil {
		return "", nil, err
	}
	executable := i.Executable
	if runtime.GOOS != "windows" {
		return executable, args, nil
	}
	native, prefix, err := resolveWindowsCursorNative(executable)
	if err != nil {
		return "", nil, err
	}
	return native, append(prefix, args...), nil
}

func resolveWindowsCursorNative(executable string) (string, []string, error) {
	resolved, err := exec.LookPath(executable)
	if err != nil {
		if filepath.IsAbs(executable) {
			if info, statErr := os.Stat(executable); statErr == nil && !info.IsDir() {
				resolved = executable
				err = nil
			}
		}
		if err != nil {
			return "", nil, fmt.Errorf("resolve Cursor executable: %w", err)
		}
	}
	resolved, err = filepath.Abs(filepath.Clean(resolved))
	if err != nil {
		return "", nil, fmt.Errorf("canonicalize Cursor executable: %w", err)
	}
	if strings.EqualFold(filepath.Ext(resolved), ".exe") {
		return resolved, nil, nil
	}
	root := filepath.Dir(resolved)
	if node, index, ok := cursorNativeFiles(root); ok {
		return node, []string{index}, nil
	}
	versions := filepath.Join(root, "versions")
	entries, readErr := os.ReadDir(versions)
	if readErr != nil {
		return "", nil, fmt.Errorf("Cursor native multiline transport is unavailable: %w", readErr)
	}
	selected := ""
	var selectedTime time.Time
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(versions, entry.Name())
		if _, _, ok := cursorNativeFiles(candidate); !ok {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		modified := info.ModTime()
		if selected == "" || modified.After(selectedTime) || modified.Equal(selectedTime) && entry.Name() > selected {
			selected, selectedTime = entry.Name(), modified
		}
	}
	if selected == "" {
		return "", nil, errors.New("Cursor native multiline transport is unavailable")
	}
	node, index, _ := cursorNativeFiles(filepath.Join(versions, selected))
	return node, []string{index}, nil
}

func cursorNativeFiles(root string) (string, string, bool) {
	node, index := filepath.Join(root, "node.exe"), filepath.Join(root, "index.js")
	nodeInfo, nodeErr := os.Stat(node)
	indexInfo, indexErr := os.Stat(index)
	return node, index, nodeErr == nil && indexErr == nil && nodeInfo.Mode().IsRegular() && indexInfo.Mode().IsRegular()
}

type LaunchRequest struct {
	Workspace       string
	Model           string
	Mode            string
	Permission      string
	TrustAuthorized bool
	MCPIsolation    string
	MCPAllowlist    []string
	Prompt          string
	TaskCanary      string
	TranscriptPath  string
	FrozenInputs    []orchestration.FrozenInputRef
}

type ExecutionResult struct {
	Status             string
	Reason             string
	ProviderHandle     string
	ProviderSessionID  string
	ProviderPromptHash string
	Terminal           bool
	ExecutionStarted   *bool
	SideEffectState    string
	Identity           map[string]string
	TaskCanary         string
	FinalText          string
	FinalSource        string
	RawEventRefs       []string
	TranscriptPath     string
	TranscriptWritten  bool
	ExitCode           int
	Stdout             []byte
	Stderr             []byte
}

func (r ExecutionResult) HostActionResult() orchestration.HostActionResult {
	return orchestration.HostActionResult{
		Status:             r.Status,
		Reason:             r.Reason,
		ProviderHandle:     r.ProviderHandle,
		ProviderSessionID:  r.ProviderSessionID,
		ProviderPromptHash: r.ProviderPromptHash,
		Terminal:           r.Terminal,
		ExecutionStarted:   r.ExecutionStarted,
		SideEffectState:    r.SideEffectState,
		Identity:           copyMap(r.Identity),
		TaskCanary:         r.TaskCanary,
		RawEventRefs:       append([]string(nil), r.RawEventRefs...),
		FinalText:          r.FinalText,
	}
}

// Launch starts one Cursor turn. It waits only for a provider session identity
// (or a terminal failure), then returns a running result so the core can issue
// an observe action. The process continues under the runner's context.
func (a *Adapter) Launch(ctx context.Context, request LaunchRequest) (ExecutionResult, error) {
	if ctx == nil {
		return ExecutionResult{}, errors.New("context is required")
	}
	normalized, err := normalizeLaunchRequest(request)
	if err != nil {
		return ExecutionResult{}, err
	}
	executable, args, err := (Invocation{Executable: a.executable(), Workspace: normalized.Workspace, Model: normalized.Model, Mode: normalized.Mode, Trust: normalized.TrustAuthorized, AdditionalReadDirs: frozenInputRoots(normalized.FrozenInputs)}).Command(normalized.Prompt)
	if err != nil {
		return ExecutionResult{}, err
	}
	turn, err := a.startTurn(ctx, normalized, executable, args, "", normalized.Prompt)
	if err != nil {
		return ExecutionResult{}, err
	}
	handleID := orchestration.NewID("cursor-process")
	s := &session{handleID: handleID, request: normalized, providerHandle: handleID, current: turn}
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
		if actual, count := turn.receivedPrompt(); count != 1 || actual != normalized.Prompt {
			turn.process.Cancel()
			select {
			case <-turn.done:
				return s.terminalResult(turn), nil
			case <-time.After(2 * time.Second):
				return s.result("cancel_unconfirmed", "prompt_transport_mismatch_cancel_unconfirmed", false), nil
			}
		}
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
	request.Mode = normalizedMode(request.Mode)
	request.Permission = normalizedPermission(request.Permission)
	if request.Model == "" || strings.TrimSpace(request.Prompt) == "" || strings.TrimSpace(request.TaskCanary) == "" {
		return LaunchRequest{}, errors.New("model, prompt and task canary are required")
	}
	if request.Mode != "plan" && request.Mode != "ask" {
		return LaunchRequest{}, errors.New("Cursor launch mode must be plan or ask")
	}
	if request.Permission != "read-only" {
		return LaunchRequest{}, errors.New("Cursor launch requires read-only permission in Wave 3")
	}
	if len(request.MCPAllowlist) > 0 {
		return LaunchRequest{}, errors.New("Cursor launch requires a verified task MCP isolation boundary")
	}
	if !request.TrustAuthorized {
		return LaunchRequest{}, errors.New("workspace trust authorization is required before launch")
	}
	if request.TranscriptPath != "" && !filepath.IsAbs(request.TranscriptPath) {
		return LaunchRequest{}, errors.New("transcript path must be absolute")
	}
	verifiedInputs, err := orchestration.VerifyFrozenInputs(request.FrozenInputs)
	if err != nil {
		return LaunchRequest{}, fmt.Errorf("verify frozen read-only inputs: %w", err)
	}
	request.FrozenInputs = verifiedInputs
	return request, nil
}

type SendRequest struct {
	HandleID       string
	ActionID       string
	Prompt         string
	CoreAuthorized bool
	TaskCanary     string
}

// Send is intentionally impossible without a core-issued action ID and an
// explicit authorization bit. A provider exit code cannot cause an implicit
// delivery-only follow-up.
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
		return ExecutionResult{}, errors.New("Cursor session is still running")
	}
	if s.contaminated {
		s.mu.Unlock()
		return ExecutionResult{}, errors.New("Cursor session is contaminated")
	}
	if s.sendInFlight {
		s.mu.Unlock()
		return ExecutionResult{}, errors.New("Cursor session already has a follow-up in flight")
	}
	if s.providerSessionID == "" {
		s.mu.Unlock()
		return ExecutionResult{}, errors.New("Cursor session identity is missing")
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
	executable, args, err := (Invocation{Executable: a.executable(), Workspace: base.Workspace, Model: base.Model, Mode: base.Mode, Trust: base.TrustAuthorized, ResumeSessionID: providerSessionID, AdditionalReadDirs: frozenInputRoots(base.FrozenInputs)}).Command(request.Prompt)
	if err != nil {
		s.rollbackSend(request.ActionID)
		return ExecutionResult{}, err
	}
	turn, err := a.startTurn(ctx, base, executable, args, providerSessionID, request.Prompt)
	if err != nil {
		s.rollbackSend(request.ActionID)
		return ExecutionResult{}, err
	}
	s.mu.Lock()
	s.current = turn
	s.sendInFlight = false
	s.mu.Unlock()
	go a.consumeTurn(s, turn)
	startupTimeout := a.StartupTimeout
	if startupTimeout <= 0 {
		startupTimeout = DefaultStartupTimeout
	}
	timer := time.NewTimer(startupTimeout)
	defer timer.Stop()
	select {
	case <-turn.ready:
		if actual, count := turn.receivedPrompt(); count != 1 || actual != request.Prompt {
			turn.process.Cancel()
			select {
			case <-turn.done:
				return s.terminalResult(turn), nil
			case <-time.After(2 * time.Second):
				return s.result("cancel_unconfirmed", "prompt_transport_mismatch_cancel_unconfirmed", false), nil
			}
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
			return s.result("cancel_unconfirmed", "send_context_cancelled", false), nil
		}
	case <-timer.C:
		turn.process.CancelWithCause(context.DeadlineExceeded)
		select {
		case <-turn.done:
			return s.terminalResult(turn), nil
		case <-time.After(2 * time.Second):
			return s.result("cancel_unconfirmed", "send_startup_timeout_cancel_unconfirmed", false), nil
		}
	}
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
		return ExecutionResult{}, errors.New("Cursor session has no active turn")
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
		return ExecutionResult{}, errors.New("Cursor session has no active turn")
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
		return errors.New("cannot cleanup a running Cursor session")
	}
	a.mu.Lock()
	delete(a.sessions, s.handleID)
	delete(a.sessions, providerSessionID)
	a.mu.Unlock()
	return nil
}

type CleanupRequest struct {
	HandleID string
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
	expectedPrompt  string
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

func (a *Adapter) startTurn(ctx context.Context, request LaunchRequest, executable string, args []string, expectedSession, expectedPrompt string) (*turn, error) {
	if a.Runner == nil {
		a.Runner = cli.NewRunner()
	}
	process, err := a.Runner.Start(ctx, cli.Command{Executable: executable, Args: args, Dir: request.Workspace, Environment: append([]string(nil), a.Environment...)})
	if err != nil {
		return nil, err
	}
	turn := &turn{process: process, expectedSession: expectedSession, expectedPrompt: expectedPrompt, transcriptPath: request.TranscriptPath, done: make(chan struct{}), ready: make(chan struct{})}
	return turn, nil
}

func (a *Adapter) consumeTurn(s *session, turn *turn) {
	for {
		a.drainTurnOutput(turn)
		select {
		case <-turn.process.Finished():
			// Finished is published only after the runner flushes its final
			// collector line. Drain it before classifying the terminal result.
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
		promptObserved := turn.summary.UserEventCount > 0
		turn.mu.Unlock()
		if sessionID != "" && promptObserved {
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

func (t *turn) receivedPrompt() (string, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.summary.ReceivedPrompt, t.summary.UserEventCount
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
	providerPromptHash := ""
	if s.current != nil {
		if prompt, count := s.current.receivedPrompt(); count == 1 && prompt != "" {
			providerPromptHash = hashText(prompt)
		}
	}
	return ExecutionResult{Status: "running", Reason: "provider_session_started", ProviderHandle: s.providerHandle, ProviderSessionID: s.providerSessionID, ProviderPromptHash: providerPromptHash, Terminal: false, ExecutionStarted: &started, SideEffectState: "active", Identity: identityMap(s), TaskCanary: s.request.TaskCanary, TranscriptPath: s.request.TranscriptPath}
}

func (s *session) terminalResult(turn *turn) ExecutionResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	turn.mu.Lock()
	summary := turn.summary
	processResult := turn.result
	transcriptWritten := turn.transcriptWritten
	turn.mu.Unlock()
	observedSessionID := summary.SessionID
	// The provider session ID is local resume authority. Only an initial or
	// expected matching response may update it; a mismatching resume response
	// remains visible in returned evidence without becoming the ID used by a
	// later Send.
	if observedSessionID != "" && (turn.expectedSession == "" || observedSessionID == turn.expectedSession) {
		s.providerSessionID = observedSessionID
	}
	if summary.UserEventCount > 0 && (summary.UserEventCount != 1 || summary.ReceivedPrompt != turn.expectedPrompt) {
		return s.resultLocked("failed", "prompt_transport_mismatch", true, summary, processResult, transcriptWritten, observedSessionID)
	}
	if processResult.Cancelled {
		status := "cancelled"
		reason := "process_cancelled"
		if processResult.TimedOut {
			status, reason = "timed_out", "process_timeout"
		}
		return s.resultLocked(status, reason, true, summary, processResult, transcriptWritten, observedSessionID)
	}
	if summary.UserEventCount == 0 {
		return s.resultLocked("failed", "provider_prompt_evidence_missing", true, summary, processResult, transcriptWritten, observedSessionID)
	}
	if processResult.ExitCode != 0 {
		return s.resultLocked("failed", "process_exit", true, summary, processResult, transcriptWritten, observedSessionID)
	}
	if summary.SessionID == "" {
		return s.resultLocked("failed", "session_identity_missing", true, summary, processResult, transcriptWritten, "")
	}
	if turn.expectedSession != "" && summary.SessionID != turn.expectedSession {
		s.contaminated = true
		return s.resultLocked("contaminated", "session_identity_mismatch", true, summary, processResult, transcriptWritten, observedSessionID)
	}
	if providerTerminalError(summary.TerminalStatus) {
		return s.resultLocked("failed", "provider_terminal_error", true, summary, processResult, transcriptWritten, observedSessionID)
	}
	if !summary.Terminal {
		return s.resultLocked("failed", "terminal_event_missing", true, summary, processResult, transcriptWritten, observedSessionID)
	}
	if !summary.DeliveryComplete() {
		return s.resultLocked("failed", "delivery_missing", true, summary, processResult, transcriptWritten, observedSessionID)
	}
	return s.resultLocked("completed", "terminal_and_final_delivery", true, summary, processResult, transcriptWritten, observedSessionID)
}

func (s *session) result(status, reason string, terminal bool) ExecutionResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	providerPromptHash := ""
	if s.current != nil {
		if prompt, count := s.current.receivedPrompt(); count == 1 && prompt != "" {
			providerPromptHash = hashText(prompt)
		}
	}
	return ExecutionResult{Status: status, Reason: reason, ProviderHandle: s.providerHandle, ProviderSessionID: s.providerSessionID, ProviderPromptHash: providerPromptHash, Terminal: terminal, ExecutionStarted: boolPtr(true), SideEffectState: "unknown", Identity: identityMap(s), TaskCanary: s.request.TaskCanary, TranscriptPath: s.request.TranscriptPath}
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
	providerPromptHash := ""
	if summary.UserEventCount == 1 && summary.ReceivedPrompt != "" {
		providerPromptHash = hashText(summary.ReceivedPrompt)
	}
	return ExecutionResult{Status: status, Reason: reason, ProviderHandle: s.providerHandle, ProviderSessionID: identitySessionID, ProviderPromptHash: providerPromptHash, Terminal: terminal, ExecutionStarted: boolPtr(true), SideEffectState: sideEffectFor(status), Identity: identityMapFor(s, identitySessionID), TaskCanary: s.request.TaskCanary, FinalText: summary.FinalText, FinalSource: summary.FinalSource, RawEventRefs: refs, TranscriptPath: s.request.TranscriptPath, TranscriptWritten: transcriptWritten, ExitCode: processResult.ExitCode, Stdout: append([]byte(nil), processResult.Stdout...), Stderr: append([]byte(nil), processResult.Stderr...)}
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

func (s *session) rollbackSend(actionID string) {
	s.mu.Lock()
	delete(s.sendActions, actionID)
	s.sendInFlight = false
	s.mu.Unlock()
}

func providerTerminalError(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "error", "failed", "failure", "cancelled", "canceled", "aborted":
		return true
	default:
		return false
	}
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

func hashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func frozenInputRoots(inputs []orchestration.FrozenInputRef) []string {
	seen := map[string]struct{}{}
	roots := make([]string, 0, len(inputs))
	for _, input := range inputs {
		root := filepath.Clean(input.Root)
		key := strings.ToLower(root)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		roots = append(roots, root)
	}
	return roots
}

func (a *Adapter) findSession(id string) (*session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("Cursor session handle is required")
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
		return nil, fmt.Errorf("unknown Cursor session %q", id)
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
	temporary, err := os.CreateTemp(parent, ".cursor-transcript-*")
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
		return nil, errors.New("provider identity does not match Cursor adapter")
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
