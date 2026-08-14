package cursor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-workflow-toolkit/internal/orchestration"
)

// TestRealCursorAdapterSmoke is opt-in because it performs a real Cursor
// Agent request. It intentionally uses plan mode, explicit trust and a
// no-tool prompt; it never enables force/yolo, sandbox enabled mode or MCP
// auto-approval.
func TestRealCursorAdapterSmoke(t *testing.T) {
	if os.Getenv("CURSOR_ADAPTER_REAL_SMOKE") != "1" {
		t.Skip("set CURSOR_ADAPTER_REAL_SMOKE=1 to run a real Cursor Adapter call")
	}
	workspace := os.Getenv("CURSOR_ADAPTER_SMOKE_WORKSPACE")
	if workspace == "" {
		t.Fatal("CURSOR_ADAPTER_SMOKE_WORKSPACE is required")
	}
	model := os.Getenv("CURSOR_ADAPTER_SMOKE_MODEL")
	if model == "" {
		model = "cursor-grok-4.5-high"
	}
	executable := os.Getenv("CURSOR_ADAPTER_EXECUTABLE")
	if executable == "" {
		executable = DefaultExecutable
	}
	adapter := New(executable)
	adapter.StartupTimeout = DefaultStartupTimeout
	probeContext, cancelProbe := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancelProbe()
	preflight, err := adapter.Preflight(probeContext, PreflightRequest{Workspace: workspace, Model: model, Mode: "plan", Permission: "read-only", TrustAuthorized: true})
	if err != nil {
		t.Fatalf("Cursor preflight failed to start: %v", err)
	}
	if preflight.Status != "passed" {
		t.Fatalf("Cursor preflight was not admitted: %#v", preflight)
	}
	canonicalWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatal(err)
	}
	requestContext, cancelRequest := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancelRequest()
	running, err := adapter.Launch(requestContext, LaunchRequest{Workspace: canonicalWorkspace, Model: model, Mode: "plan", Permission: "read-only", TrustAuthorized: true, Prompt: "Do not read files or call tools. Return exactly ADAPTER_SMOKE_OK and nothing else.", TaskCanary: "real-cursor-smoke-canary"})
	if err != nil {
		t.Fatalf("Cursor launch failed to start: %v", err)
	}
	deadline := time.Now().Add(4 * time.Minute)
	var terminal ExecutionResult
	for time.Now().Before(deadline) {
		terminal, err = adapter.Observe(requestContext, ObserveRequest{HandleID: running.ProviderHandle})
		if err != nil {
			t.Fatalf("Cursor observe failed: %v", err)
		}
		if terminal.Terminal {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Logf("status=%s reason=%s source=%s session_present=%t terminal=%t exit_code=%d final_bytes=%d", terminal.Status, terminal.Reason, terminal.FinalSource, terminal.ProviderSessionID != "", terminal.Terminal, terminal.ExitCode, len(terminal.FinalText))
	if terminal.Status != "completed" || terminal.FinalText != "ADAPTER_SMOKE_OK" {
		t.Fatalf("Cursor delivery was not completed: status=%s reason=%s source=%s session_present=%t terminal=%t exit_code=%d final_bytes=%d", terminal.Status, terminal.Reason, terminal.FinalSource, terminal.ProviderSessionID != "", terminal.Terminal, terminal.ExitCode, len(terminal.FinalText))
	}
}

// TestRealCursorFrozenInputRead proves plan mode can read one framework-
// verified artifact through the explicit --add-dir root without Shell.
func TestRealCursorFrozenInputRead(t *testing.T) {
	if os.Getenv("CURSOR_FROZEN_INPUT_REAL_SMOKE") != "1" {
		t.Skip("set CURSOR_FROZEN_INPUT_REAL_SMOKE=1 to run the real Cursor frozen-input read check")
	}
	workspace := os.Getenv("CURSOR_ADAPTER_SMOKE_WORKSPACE")
	artifactPath := os.Getenv("CURSOR_PROJECT_ROLE_FROZEN_INPUT")
	if workspace == "" || artifactPath == "" {
		t.Fatal("CURSOR_ADAPTER_SMOKE_WORKSPACE and CURSOR_PROJECT_ROLE_FROZEN_INPUT are required")
	}
	canonicalWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatal(err)
	}
	artifactPath, err = filepath.Abs(filepath.Clean(artifactPath))
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	head := ""
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "HEAD: `") && strings.HasSuffix(line, "`") {
			head = strings.TrimSuffix(strings.TrimPrefix(line, "HEAD: `"), "`")
			break
		}
	}
	if len(head) != 40 {
		t.Fatal("frozen input has no 40-character HEAD")
	}
	sum := sha256.Sum256(content)
	input := orchestration.FrozenInputRef{Name: "repository-review", Path: artifactPath, Root: filepath.Dir(artifactPath), Format: "markdown", Bytes: int64(len(content)), SHA256: hex.EncodeToString(sum[:])}
	model := os.Getenv("CURSOR_ADAPTER_SMOKE_MODEL")
	if model == "" {
		model = "cursor-grok-4.5-high"
	}
	executable := os.Getenv("CURSOR_ADAPTER_EXECUTABLE")
	if executable == "" {
		executable = DefaultExecutable
	}
	adapter := New(executable)
	adapter.StartupTimeout = DefaultStartupTimeout
	requestContext, cancelRequest := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancelRequest()
	expected := "FROZEN_INPUT_OK:" + head
	prompt := "Use read-only file tools to open this exact artifact: " + artifactPath + "\nDo not use Shell. Read its Repository identity and return exactly " + expected + " and nothing else."
	running, err := adapter.Launch(requestContext, LaunchRequest{Workspace: canonicalWorkspace, Model: model, Mode: "plan", Permission: "read-only", TrustAuthorized: true, Prompt: prompt, TaskCanary: "real-cursor-frozen-input", FrozenInputs: []orchestration.FrozenInputRef{input}})
	if err != nil {
		t.Fatalf("Cursor frozen-input launch failed: %v", err)
	}
	defer func() { _ = adapter.Cleanup(CleanupRequest{HandleID: running.ProviderHandle}) }()
	deadline := time.Now().Add(4 * time.Minute)
	terminal := running
	for !terminal.Terminal && time.Now().Before(deadline) {
		terminal, err = adapter.Observe(requestContext, ObserveRequest{HandleID: running.ProviderHandle})
		if err != nil {
			t.Fatalf("Cursor frozen-input observe failed: %v", err)
		}
		if !terminal.Terminal {
			time.Sleep(250 * time.Millisecond)
		}
	}
	if terminal.Status != "completed" || !strings.Contains(terminal.FinalText, expected) {
		t.Fatalf("Cursor frozen-input result status=%s reason=%s final_bytes=%d head_present=%t", terminal.Status, terminal.Reason, len(terminal.FinalText), strings.Contains(terminal.FinalText, head))
	}
	t.Logf("Frozen input read PASS bytes=%d sha256=%s head=%s final_bytes=%d", len(content), input.SHA256, head, len(terminal.FinalText))
}

// TestRealCursorPromptTransportExactness proves the Provider-visible user
// event is byte-for-byte identical to a single multiline Prompt. It covers the
// whitespace and option-like content that was lost by the old argv splitting.
func TestRealCursorPromptTransportExactness(t *testing.T) {
	if os.Getenv("CURSOR_PROMPT_EXACTNESS_REAL_SMOKE") != "1" {
		t.Skip("set CURSOR_PROMPT_EXACTNESS_REAL_SMOKE=1 to run the real Cursor Prompt transport check")
	}
	workspace := os.Getenv("CURSOR_ADAPTER_SMOKE_WORKSPACE")
	if workspace == "" {
		t.Fatal("CURSOR_ADAPTER_SMOKE_WORKSPACE is required")
	}
	model := os.Getenv("CURSOR_ADAPTER_SMOKE_MODEL")
	if model == "" {
		model = "cursor-grok-4.5-high"
	}
	executable := os.Getenv("CURSOR_ADAPTER_EXECUTABLE")
	if executable == "" {
		executable = DefaultExecutable
	}
	canonicalWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatal(err)
	}
	prompt := "--transport-exactness\n# Markdown heading\n\n  indented line\n\n```text\n--leading-option-like-token\n  keep two spaces\n```\n\nDo not read files or call tools. Return exactly CURSOR_PROMPT_EXACT_OK and nothing else."
	sum := sha256.Sum256([]byte(prompt))
	expectedHash := hex.EncodeToString(sum[:])

	adapter := New(executable)
	adapter.StartupTimeout = DefaultStartupTimeout
	requestContext, cancelRequest := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancelRequest()
	running, err := adapter.Launch(requestContext, LaunchRequest{Workspace: canonicalWorkspace, Model: model, Mode: "plan", Permission: "read-only", TrustAuthorized: true, Prompt: prompt, TaskCanary: "real-cursor-prompt-exactness"})
	if err != nil {
		t.Fatalf("Cursor exactness launch failed: %v", err)
	}
	if running.ProviderPromptHash != expectedHash {
		t.Fatalf("Provider Prompt hash mismatch at launch: expected=%s actual=%s", expectedHash, running.ProviderPromptHash)
	}
	defer func() { _ = adapter.Cleanup(CleanupRequest{HandleID: running.ProviderHandle}) }()
	deadline := time.Now().Add(4 * time.Minute)
	terminal := running
	for !terminal.Terminal && time.Now().Before(deadline) {
		terminal, err = adapter.Observe(requestContext, ObserveRequest{HandleID: running.ProviderHandle})
		if err != nil {
			t.Fatalf("Cursor exactness observe failed: %v", err)
		}
		if !terminal.Terminal {
			time.Sleep(250 * time.Millisecond)
		}
	}
	if terminal.Status != "completed" || terminal.FinalText != "CURSOR_PROMPT_EXACT_OK" || terminal.ProviderPromptHash != expectedHash {
		t.Fatalf("Cursor exactness result status=%s reason=%s final=%q expected_hash=%s actual_hash=%s", terminal.Status, terminal.Reason, terminal.FinalText, expectedHash, terminal.ProviderPromptHash)
	}
	t.Logf("Prompt exactness PASS bytes=%d sha256=%s session_present=%t", len(prompt), expectedHash, terminal.ProviderSessionID != "")
}
