package codexcli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRealCodexDeliverySmoke is intentionally opt-in because it performs a
// real provider call. It is the regression test for the observed exit-0 /
// empty-stdout condition and must run with the same registered model binding
// used by the target environment.
func TestRealCodexDeliverySmoke(t *testing.T) {
	if os.Getenv("CODEX_ADAPTER_REAL_SMOKE") != "1" {
		t.Skip("set CODEX_ADAPTER_REAL_SMOKE=1 to run a real Codex CLI call")
	}
	workspace := os.Getenv("CODEX_ADAPTER_SMOKE_WORKSPACE")
	if workspace == "" {
		t.Fatal("CODEX_ADAPTER_SMOKE_WORKSPACE is required for the real smoke")
	}
	model := os.Getenv("CODEX_ADAPTER_SMOKE_MODEL")
	if model == "" {
		model = "gpt-5.6-luna"
	}
	executable := os.Getenv("CODEX_ADAPTER_EXECUTABLE")
	if executable == "" {
		executable = "codex"
	}
	provider, providerBaseURL, providerWireAPI, providerAuth := realProviderConfig()
	lastMessagePath := filepath.Join(t.TempDir(), "last-message.txt")
	result, err := (Invocation{
		Executable:            executable,
		Workspace:             workspace,
		Model:                 model,
		ModelProvider:         provider,
		ProviderBaseURL:       providerBaseURL,
		ProviderWireAPI:       providerWireAPI,
		ProviderAuth:          providerAuth,
		OutputLastMessagePath: lastMessagePath,
		Ephemeral:             true,
	}).Run(t.Context(), "Adapter smoke test. Do not call tools, read files, or edit anything. Return exactly the token ADAPTER_SMOKE_OK and nothing else.")
	if err != nil {
		t.Fatalf("real Codex Adapter call failed to start: %v", err)
	}
	t.Logf("status=%s reason=%s source=%s exit_code=%d final_bytes=%d stdout_bytes=%d", result.Status, result.Reason, result.Source, result.ExitCode, len(result.FinalText), len(result.Stdout))
	if result.Status != StatusCompleted || result.FinalText != "ADAPTER_SMOKE_OK" {
		t.Fatalf("Codex delivery was not completed: %#v", result)
	}
}

// TestRealCodexAdapterLifecycleSmoke is intentionally separate from the
// direct durable-delivery regression. It proves the resumable Adapter path on
// the real CLI: preflight, non-ephemeral Launch, thread identity, Observe and
// terminal delivery. It remains opt-in because it creates a real provider
// session.
func TestRealCodexAdapterLifecycleSmoke(t *testing.T) {
	if os.Getenv("CODEX_ADAPTER_REAL_LIFECYCLE_SMOKE") != "1" {
		t.Skip("set CODEX_ADAPTER_REAL_LIFECYCLE_SMOKE=1 to run a real Codex Adapter lifecycle call")
	}
	workspace := os.Getenv("CODEX_ADAPTER_SMOKE_WORKSPACE")
	if workspace == "" {
		t.Fatal("CODEX_ADAPTER_SMOKE_WORKSPACE is required for the real lifecycle smoke")
	}
	model := os.Getenv("CODEX_ADAPTER_SMOKE_MODEL")
	if model == "" {
		model = "gpt-5.6-luna"
	}
	reasoning := os.Getenv("CODEX_ADAPTER_SMOKE_REASONING")
	if reasoning == "" {
		reasoning = "max"
	}
	executable := os.Getenv("CODEX_ADAPTER_EXECUTABLE")
	if executable == "" {
		executable = "codex"
	}
	provider, providerBaseURL, providerWireAPI, providerAuth := realProviderConfig()
	adapter := New(executable)
	adapter.ModelProvider = provider
	adapter.ProviderBaseURL = providerBaseURL
	adapter.ProviderWireAPI = providerWireAPI
	adapter.ProviderAuth = providerAuth
	adapter.StartupTimeout = 90 * time.Second
	probeContext, cancelProbe := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancelProbe()
	preflight, err := adapter.Preflight(probeContext, PreflightRequest{
		Workspace: workspace, Model: model, Reasoning: reasoning, Permission: DefaultPermission,
		Sandbox: DefaultSandbox, Approval: DefaultApproval, TrustAuthorized: true,
	})
	if err != nil {
		t.Fatalf("real Codex Adapter preflight failed to start: %v", err)
	}
	if preflight.Status != "passed" || !preflight.ModelRegistered {
		t.Fatalf("real Codex Adapter preflight was not admitted: %#v", preflight)
	}

	requestContext, cancelRequest := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancelRequest()
	transcriptPath := filepath.Join(t.TempDir(), "codex-lifecycle.jsonl")
	running, err := adapter.Launch(requestContext, LaunchRequest{
		Workspace: workspace, Model: model, Reasoning: reasoning, Permission: DefaultPermission,
		Sandbox: DefaultSandbox, Approval: DefaultApproval, TrustAuthorized: true,
		Prompt:     "Real Adapter lifecycle smoke. Do not call tools, read files, or edit anything. Return exactly the token ADAPTER_SMOKE_OK and nothing else.",
		TaskCanary: "real-codex-adapter-lifecycle-smoke-canary", TranscriptPath: transcriptPath,
	})
	if err != nil {
		t.Fatalf("real Codex Adapter launch failed to start: %v", err)
	}
	defer func() {
		if err := adapter.Cleanup(CleanupRequest{HandleID: running.ProviderHandle}); err != nil {
			t.Logf("real Codex Adapter cleanup: %v", err)
		}
	}()
	terminal := observeRealCodexLifecycle(t, adapter, requestContext, running.ProviderHandle)
	t.Logf("version=%s launch_status=%s launch_thread_present=%t terminal_status=%s terminal_reason=%s final_source=%s terminal=%t exit_code=%d final_bytes=%d durable_last_message=%t transcript=%t", preflight.Version, running.Status, running.ProviderSessionID != "", terminal.Status, terminal.Reason, terminal.FinalSource, terminal.Terminal, terminal.ExitCode, len(terminal.FinalText), terminal.LastMessageAvailable, terminal.TranscriptWritten)
	if running.Status != "running" || running.ProviderSessionID == "" || terminal.Status != "completed" || terminal.Reason != "terminal_and_final_delivery" || terminal.FinalText != "ADAPTER_SMOKE_OK" || !terminal.Terminal {
		t.Fatalf("real Codex Adapter lifecycle delivery was not completed: launch=%#v terminal=%#v", running, terminal)
	}
}

func observeRealCodexLifecycle(t *testing.T, adapter *Adapter, ctx context.Context, handle string) ExecutionResult {
	t.Helper()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var latest ExecutionResult
	for {
		observed, err := adapter.Observe(ctx, ObserveRequest{HandleID: handle})
		if err != nil {
			cancelContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = adapter.Cancel(cancelContext, CancelRequest{HandleID: handle})
			cancel()
			t.Fatalf("real Codex Adapter observe failed: %v", err)
		}
		latest = observed
		if latest.Terminal {
			return latest
		}
		select {
		case <-ctx.Done():
			cancelContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = adapter.Cancel(cancelContext, CancelRequest{HandleID: handle})
			cancel()
			t.Fatalf("real Codex Adapter observe timed out: %#v", latest)
		case <-ticker.C:
		}
	}
}

func realProviderConfig() (provider, baseURL, wireAPI string, requiresAuth bool) {
	provider = os.Getenv("CODEX_ADAPTER_SMOKE_MODEL_PROVIDER")
	baseURL = os.Getenv("CODEX_ADAPTER_SMOKE_PROVIDER_BASE_URL")
	wireAPI = os.Getenv("CODEX_ADAPTER_SMOKE_PROVIDER_WIRE_API")
	requiresAuth = os.Getenv("CODEX_ADAPTER_SMOKE_PROVIDER_AUTH") == "1"
	return provider, baseURL, wireAPI, requiresAuth
}
