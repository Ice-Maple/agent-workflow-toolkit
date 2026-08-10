package orchestration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agent-workflow-toolkit/internal/adapters/claude"
	"agent-workflow-toolkit/internal/adapters/codexcli"
	"agent-workflow-toolkit/internal/adapters/cursor"
)

const wave4SmokeToken = "ADAPTER_SMOKE_OK"

type lifecycleResult struct {
	status    string
	reason    string
	handle    string
	sessionID string
	finalText string
	terminal  bool
}

type lifecycleCase struct {
	name      string
	preflight func(context.Context) (string, error)
	launch    func(context.Context) (lifecycleResult, error)
	send      func(context.Context, string) (lifecycleResult, error)
	observe   func(context.Context, string) (lifecycleResult, error)
	cleanup   func(string) error
}

func TestWave4ExternalAdapterLifecycleConformance(t *testing.T) {
	for _, makeCase := range []func(*testing.T) lifecycleCase{
		newCursorLifecycleCase,
		newClaudeLifecycleCase,
		newCodexLifecycleCase,
	} {
		adapterCase := makeCase(t)
		t.Run(adapterCase.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			status, err := adapterCase.preflight(ctx)
			if err != nil {
				t.Fatalf("preflight error = %v", err)
			}
			if status != "passed" {
				t.Fatalf("preflight status = %q, want passed", status)
			}

			running, err := adapterCase.launch(ctx)
			if err != nil {
				t.Fatalf("launch error = %v", err)
			}
			if running.status != "running" || running.handle == "" || running.sessionID == "" {
				t.Fatalf("launch result = %#v", running)
			}

			var terminal lifecycleResult
			for {
				terminal, err = adapterCase.observe(ctx, running.handle)
				if err != nil {
					t.Fatalf("observe error = %v", err)
				}
				if terminal.terminal {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatalf("observe did not reach terminal result: %#v", terminal)
				case <-time.After(10 * time.Millisecond):
				}
			}
			if terminal.status != "completed" || terminal.finalText != wave4SmokeToken || terminal.reason == "" {
				t.Fatalf("terminal result = %#v", terminal)
			}
			if terminal.sessionID != running.sessionID {
				t.Fatalf("session changed from %q to %q", running.sessionID, terminal.sessionID)
			}

			followUp, err := adapterCase.send(ctx, running.handle)
			if err != nil {
				t.Fatalf("follow-up send error = %v", err)
			}
			if followUp.handle == "" || followUp.sessionID != running.sessionID || followUp.status != "running" {
				t.Fatalf("follow-up running result = %#v", followUp)
			}
			for {
				followUp, err = adapterCase.observe(ctx, followUp.handle)
				if err != nil {
					t.Fatalf("follow-up observe error = %v", err)
				}
				if followUp.terminal {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatalf("follow-up observe did not reach terminal result: %#v", followUp)
				case <-time.After(10 * time.Millisecond):
				}
			}
			if followUp.status != "completed" || followUp.finalText != wave4SmokeToken || followUp.sessionID != running.sessionID || followUp.reason == "" {
				t.Fatalf("follow-up terminal result = %#v", followUp)
			}
			if err := adapterCase.cleanup(followUp.handle); err != nil {
				t.Fatalf("cleanup error = %v", err)
			}
		})
	}
}

func newCursorLifecycleCase(t *testing.T) lifecycleCase {
	workspace := t.TempDir()
	adapter := cursor.New(os.Args[0])
	adapter.Environment = []string{"W4_ADAPTER_HELPER=1", "W4_ADAPTER_PROVIDER=cursor"}
	return lifecycleCase{
		name: "cursor",
		preflight: func(ctx context.Context) (string, error) {
			result, err := adapter.Preflight(ctx, cursor.PreflightRequest{Workspace: workspace, Model: "fixture-model", Mode: "plan", Permission: "read-only", TrustAuthorized: true})
			return result.Status, err
		},
		launch: func(ctx context.Context) (lifecycleResult, error) {
			result, err := adapter.Launch(ctx, cursor.LaunchRequest{Workspace: workspace, Model: "fixture-model", Mode: "plan", Permission: "read-only", TrustAuthorized: true, Prompt: "return the smoke token", TaskCanary: "wave4-cursor-canary"})
			return lifecycleResult{status: result.Status, reason: result.Reason, handle: result.ProviderHandle, sessionID: result.ProviderSessionID, finalText: result.FinalText, terminal: result.Terminal}, err
		},
		send: func(ctx context.Context, handle string) (lifecycleResult, error) {
			result, err := adapter.Send(ctx, cursor.SendRequest{HandleID: handle, ActionID: "wave4-cursor-follow-up", Prompt: "return the smoke token again", CoreAuthorized: true, TaskCanary: "wave4-cursor-canary"})
			return lifecycleResult{status: result.Status, reason: result.Reason, handle: result.ProviderHandle, sessionID: result.ProviderSessionID, finalText: result.FinalText, terminal: result.Terminal}, err
		},
		observe: func(ctx context.Context, handle string) (lifecycleResult, error) {
			result, err := adapter.Observe(ctx, cursor.ObserveRequest{HandleID: handle})
			return lifecycleResult{status: result.Status, reason: result.Reason, handle: result.ProviderHandle, sessionID: result.ProviderSessionID, finalText: result.FinalText, terminal: result.Terminal}, err
		},
		cleanup: func(handle string) error { return adapter.Cleanup(cursor.CleanupRequest{HandleID: handle}) },
	}
}

func newClaudeLifecycleCase(t *testing.T) lifecycleCase {
	workspace := t.TempDir()
	adapter := claude.New(os.Args[0])
	adapter.Environment = []string{"W4_ADAPTER_HELPER=1", "W4_ADAPTER_PROVIDER=claude"}
	return lifecycleCase{
		name: "claude",
		preflight: func(ctx context.Context) (string, error) {
			result, err := adapter.Preflight(ctx, claude.PreflightRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true})
			return result.Status, err
		},
		launch: func(ctx context.Context) (lifecycleResult, error) {
			result, err := adapter.Launch(ctx, claude.LaunchRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, DisableTools: true, Prompt: "return the smoke token", TaskCanary: "wave4-claude-canary"})
			return lifecycleResult{status: result.Status, reason: result.Reason, handle: result.ProviderHandle, sessionID: result.ProviderSessionID, finalText: result.FinalText, terminal: result.Terminal}, err
		},
		send: func(ctx context.Context, handle string) (lifecycleResult, error) {
			result, err := adapter.Send(ctx, claude.SendRequest{HandleID: handle, ActionID: "wave4-claude-follow-up", Prompt: "return the smoke token again", CoreAuthorized: true, TaskCanary: "wave4-claude-canary"})
			return lifecycleResult{status: result.Status, reason: result.Reason, handle: result.ProviderHandle, sessionID: result.ProviderSessionID, finalText: result.FinalText, terminal: result.Terminal}, err
		},
		observe: func(ctx context.Context, handle string) (lifecycleResult, error) {
			result, err := adapter.Observe(ctx, claude.ObserveRequest{HandleID: handle})
			return lifecycleResult{status: result.Status, reason: result.Reason, handle: result.ProviderHandle, sessionID: result.ProviderSessionID, finalText: result.FinalText, terminal: result.Terminal}, err
		},
		cleanup: func(handle string) error { return adapter.Cleanup(claude.CleanupRequest{HandleID: handle}) },
	}
}

func newCodexLifecycleCase(t *testing.T) lifecycleCase {
	workspace := t.TempDir()
	adapter := codexcli.New(os.Args[0])
	adapter.Environment = []string{"W4_ADAPTER_HELPER=1", "W4_ADAPTER_PROVIDER=codex"}
	return lifecycleCase{
		name: "codexcli",
		preflight: func(ctx context.Context) (string, error) {
			result, err := adapter.Preflight(ctx, codexcli.PreflightRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", Sandbox: "read-only", Approval: "untrusted", TrustAuthorized: true})
			return result.Status, err
		},
		launch: func(ctx context.Context) (lifecycleResult, error) {
			result, err := adapter.Launch(ctx, codexcli.LaunchRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", Sandbox: "read-only", Approval: "untrusted", TrustAuthorized: true, OutputLastMessagePath: filepath.Join(workspace, "last-message.txt"), Prompt: "return the smoke token", TaskCanary: "wave4-codex-canary"})
			return lifecycleResult{status: result.Status, reason: result.Reason, handle: result.ProviderHandle, sessionID: result.ProviderSessionID, finalText: result.FinalText, terminal: result.Terminal}, err
		},
		send: func(ctx context.Context, handle string) (lifecycleResult, error) {
			result, err := adapter.Send(ctx, codexcli.SendRequest{HandleID: handle, ActionID: "wave4-codex-follow-up", Prompt: "return the smoke token again", CoreAuthorized: true, TaskCanary: "wave4-codex-canary"})
			return lifecycleResult{status: result.Status, reason: result.Reason, handle: result.ProviderHandle, sessionID: result.ProviderSessionID, finalText: result.FinalText, terminal: result.Terminal}, err
		},
		observe: func(ctx context.Context, handle string) (lifecycleResult, error) {
			result, err := adapter.Observe(ctx, codexcli.ObserveRequest{HandleID: handle})
			return lifecycleResult{status: result.Status, reason: result.Reason, handle: result.ProviderHandle, sessionID: result.ProviderSessionID, finalText: result.FinalText, terminal: result.Terminal}, err
		},
		cleanup: func(handle string) error { return adapter.Cleanup(codexcli.CleanupRequest{HandleID: handle}) },
	}
}

func TestMain(m *testing.M) {
	if os.Getenv("W4_ADAPTER_HELPER") == "1" {
		wave4AdapterHelper()
	}
	os.Exit(m.Run())
}

func wave4AdapterHelper() {
	provider := os.Getenv("W4_ADAPTER_PROVIDER")
	if hasArgument("--version") {
		fmt.Println("wave4-fixture 1.0")
		os.Exit(0)
	}
	if provider == "cursor" && hasArgument("models") {
		fmt.Println("fixture-model")
		os.Exit(0)
	}
	sessionID := provider + "-replay-session"
	switch provider {
	case "cursor":
		fmt.Printf(`{"type":"system","subtype":"init","session_id":%q}`+"\n", sessionID)
		fmt.Printf(`{"type":"result","subtype":"success","is_error":false,"result":%q}`+"\n", wave4SmokeToken)
	case "claude":
		fmt.Printf(`{"type":"system","subtype":"init","session_id":%q}`+"\n", sessionID)
		fmt.Printf(`{"type":"assistant","session_id":%q,"message":{"content":[{"type":"text","text":%q}]}}`+"\n", sessionID, wave4SmokeToken)
		fmt.Printf(`{"type":"result","subtype":"success","is_error":false,"result":%q,"session_id":%q}`+"\n", wave4SmokeToken, sessionID)
	case "codex":
		fmt.Printf(`{"type":"thread.started","thread_id":%q}`+"\n", sessionID)
		fmt.Printf(`{"type":"item.completed","item":{"type":"agent_message","text":%q}}`+"\n", wave4SmokeToken)
		fmt.Println(`{"type":"turn.completed"}`)
		if path := argumentValue("--output-last-message"); path != "" {
			if err := os.WriteFile(path, []byte(wave4SmokeToken+"\n"), 0o600); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(3)
			}
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown Wave 4 fixture provider")
		os.Exit(2)
	}
	os.Exit(0)
}

func hasArgument(wanted string) bool {
	for _, argument := range os.Args[1:] {
		if argument == wanted {
			return true
		}
	}
	return false
}

func argumentValue(wanted string) string {
	arguments := os.Args[1:]
	for index, argument := range arguments {
		if argument == wanted && index+1 < len(arguments) {
			return arguments[index+1]
		}
	}
	return ""
}
