package orchestration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	name           string
	setEnvironment func(...string)
	preflight      func(context.Context) (string, error)
	launch         func(context.Context) (lifecycleResult, error)
	send           func(context.Context, string, string) (lifecycleResult, error)
	observe        func(context.Context, string) (lifecycleResult, error)
	cleanup        func(string) error
}

func TestWave4ExternalAdapterDeliveryRecoveryConformance(t *testing.T) {
	for _, makeCase := range []func(*testing.T) lifecycleCase{
		newCursorLifecycleCase,
		newClaudeLifecycleCase,
		newCodexLifecycleCase,
	} {
		adapterCase := makeCase(t)
		t.Run(adapterCase.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			stateFile := filepath.Join(t.TempDir(), "recovery-count")
			adapterCase.setEnvironment("W4_ADAPTER_RECOVERY=1", "W4_ADAPTER_RECOVERY_STATE="+stateFile)

			status, err := adapterCase.preflight(ctx)
			if err != nil || status != "passed" {
				t.Fatalf("preflight = %q, err=%v", status, err)
			}
			running, err := adapterCase.launch(ctx)
			if err != nil {
				t.Fatalf("launch error = %v", err)
			}
			first := observeUntilTerminal(t, ctx, adapterCase, running.handle)
			if first.status != "failed" || first.reason != "delivery_missing" || first.sessionID == "" {
				t.Fatalf("initial delivery-missing result = %#v", first)
			}

			adapterCase.setEnvironment("W4_ADAPTER_RECOVERY=1", "W4_ADAPTER_RECOVERY_STATE="+stateFile)
			followUp, err := adapterCase.send(ctx, running.handle, "wave4-delivery-recovery")
			if err != nil {
				t.Fatalf("delivery recovery send error = %v", err)
			}
			terminal := observeUntilTerminal(t, ctx, adapterCase, followUp.handle)
			if terminal.status != "completed" || terminal.reason != "terminal_and_final_delivery" || terminal.sessionID != first.sessionID || terminal.finalText != wave4SmokeToken {
				t.Fatalf("delivery recovery terminal = %#v", terminal)
			}
			if err := adapterCase.cleanup(running.handle); err != nil {
				t.Fatalf("cleanup error = %v", err)
			}
		})
	}
}

func TestWave4ExternalAdapterContaminationConformance(t *testing.T) {
	for _, makeCase := range []func(*testing.T) lifecycleCase{
		newCursorLifecycleCase,
		newClaudeLifecycleCase,
		newCodexLifecycleCase,
	} {
		adapterCase := makeCase(t)
		t.Run(adapterCase.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			adapterCase.setEnvironment()
			status, err := adapterCase.preflight(ctx)
			if err != nil || status != "passed" {
				t.Fatalf("preflight = %q, err=%v", status, err)
			}
			running, err := adapterCase.launch(ctx)
			if err != nil {
				t.Fatalf("launch error = %v", err)
			}
			first := observeUntilTerminal(t, ctx, adapterCase, running.handle)
			if first.status != "completed" || first.sessionID == "" {
				t.Fatalf("initial result = %#v", first)
			}

			adapterCase.setEnvironment("W4_ADAPTER_CONTAMINATION=1")
			contaminatedTurn, err := adapterCase.send(ctx, running.handle, "wave4-contamination-inject")
			if err != nil {
				t.Fatalf("contamination send error = %v", err)
			}
			contaminated := observeUntilTerminal(t, ctx, adapterCase, contaminatedTurn.handle)
			if contaminated.status != "contaminated" || contaminated.reason != "session_identity_mismatch" || contaminated.sessionID == first.sessionID || contaminated.sessionID == "" {
				t.Fatalf("contamination result = %#v; first=%#v", contaminated, first)
			}
			if _, err := adapterCase.send(ctx, running.handle, "wave4-after-contamination"); err == nil {
				t.Fatal("follow-up after contamination was accepted")
			}
			if err := adapterCase.cleanup(running.handle); err != nil {
				t.Fatalf("cleanup error = %v", err)
			}
		})
	}
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

			followUp, err := adapterCase.send(ctx, running.handle, "wave4-lifecycle-follow-up")
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
	setEnvironment := func(extra ...string) {
		adapter.Environment = append([]string{"W4_ADAPTER_HELPER=1", "W4_ADAPTER_PROVIDER=cursor"}, extra...)
	}
	setEnvironment()
	return lifecycleCase{
		name:           "cursor",
		setEnvironment: setEnvironment,
		preflight: func(ctx context.Context) (string, error) {
			result, err := adapter.Preflight(ctx, cursor.PreflightRequest{Workspace: workspace, Model: "fixture-model", Mode: "plan", Permission: "read-only", TrustAuthorized: true})
			return result.Status, err
		},
		launch: func(ctx context.Context) (lifecycleResult, error) {
			result, err := adapter.Launch(ctx, cursor.LaunchRequest{Workspace: workspace, Model: "fixture-model", Mode: "plan", Permission: "read-only", TrustAuthorized: true, Prompt: "return the smoke token", TaskCanary: "wave4-cursor-canary"})
			return lifecycleResult{status: result.Status, reason: result.Reason, handle: result.ProviderHandle, sessionID: result.ProviderSessionID, finalText: result.FinalText, terminal: result.Terminal}, err
		},
		send: func(ctx context.Context, handle, actionID string) (lifecycleResult, error) {
			result, err := adapter.Send(ctx, cursor.SendRequest{HandleID: handle, ActionID: actionID, Prompt: "return the smoke token again", CoreAuthorized: true, TaskCanary: "wave4-cursor-canary"})
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
	setEnvironment := func(extra ...string) {
		adapter.Environment = append([]string{"W4_ADAPTER_HELPER=1", "W4_ADAPTER_PROVIDER=claude"}, extra...)
	}
	setEnvironment()
	return lifecycleCase{
		name:           "claude",
		setEnvironment: setEnvironment,
		preflight: func(ctx context.Context) (string, error) {
			result, err := adapter.Preflight(ctx, claude.PreflightRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true})
			return result.Status, err
		},
		launch: func(ctx context.Context) (lifecycleResult, error) {
			result, err := adapter.Launch(ctx, claude.LaunchRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", TrustAuthorized: true, DisableTools: true, Prompt: "return the smoke token", TaskCanary: "wave4-claude-canary"})
			return lifecycleResult{status: result.Status, reason: result.Reason, handle: result.ProviderHandle, sessionID: result.ProviderSessionID, finalText: result.FinalText, terminal: result.Terminal}, err
		},
		send: func(ctx context.Context, handle, actionID string) (lifecycleResult, error) {
			result, err := adapter.Send(ctx, claude.SendRequest{HandleID: handle, ActionID: actionID, Prompt: "return the smoke token again", CoreAuthorized: true, TaskCanary: "wave4-claude-canary"})
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
	setEnvironment := func(extra ...string) {
		adapter.Environment = append([]string{"W4_ADAPTER_HELPER=1", "W4_ADAPTER_PROVIDER=codex"}, extra...)
	}
	setEnvironment()
	return lifecycleCase{
		name:           "codexcli",
		setEnvironment: setEnvironment,
		preflight: func(ctx context.Context) (string, error) {
			result, err := adapter.Preflight(ctx, codexcli.PreflightRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", Sandbox: "read-only", Approval: "untrusted", TrustAuthorized: true})
			return result.Status, err
		},
		launch: func(ctx context.Context) (lifecycleResult, error) {
			result, err := adapter.Launch(ctx, codexcli.LaunchRequest{Workspace: workspace, Model: "fixture-model", Permission: "read-only", Sandbox: "read-only", Approval: "untrusted", TrustAuthorized: true, OutputLastMessagePath: filepath.Join(workspace, "last-message.txt"), Prompt: "return the smoke token", TaskCanary: "wave4-codex-canary"})
			return lifecycleResult{status: result.Status, reason: result.Reason, handle: result.ProviderHandle, sessionID: result.ProviderSessionID, finalText: result.FinalText, terminal: result.Terminal}, err
		},
		send: func(ctx context.Context, handle, actionID string) (lifecycleResult, error) {
			result, err := adapter.Send(ctx, codexcli.SendRequest{HandleID: handle, ActionID: actionID, Prompt: "return the smoke token again", CoreAuthorized: true, TaskCanary: "wave4-codex-canary"})
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
	if wave4ContaminationTurn(provider) {
		os.Exit(0)
	}
	if wave4RecoveryTurn(provider, sessionID) {
		os.Exit(0)
	}
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

func observeUntilTerminal(t *testing.T, ctx context.Context, adapterCase lifecycleCase, handle string) lifecycleResult {
	t.Helper()
	for {
		result, err := adapterCase.observe(ctx, handle)
		if err != nil {
			t.Fatalf("observe error = %v", err)
		}
		if result.terminal {
			return result
		}
		select {
		case <-ctx.Done():
			t.Fatalf("observe did not reach terminal result: %#v", result)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func wave4RecoveryTurn(provider, sessionID string) bool {
	statePath := os.Getenv("W4_ADAPTER_RECOVERY_STATE")
	if os.Getenv("W4_ADAPTER_RECOVERY") != "1" || statePath == "" {
		return false
	}
	raw, err := os.ReadFile(statePath)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "recovery state read failed", err)
		os.Exit(4)
	}
	count := 0
	if len(raw) > 0 {
		if count, err = strconv.Atoi(string(raw)); err != nil {
			fmt.Fprintln(os.Stderr, "recovery state parse failed", err)
			os.Exit(4)
		}
	}
	if err := os.WriteFile(statePath, []byte(strconv.Itoa(count+1)), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "recovery state write failed", err)
		os.Exit(4)
	}
	if count > 0 {
		return false
	}
	switch provider {
	case "cursor":
		fmt.Printf(`{"type":"system","subtype":"init","session_id":%q}`+"\n", sessionID)
		fmt.Printf(`{"type":"tool_call","name":"Read","input":{}}` + "\n")
		fmt.Println(`{"type":"turn_ended","status":"success"}`)
	case "claude":
		fmt.Printf(`{"type":"system","subtype":"init","session_id":%q}`+"\n", sessionID)
		fmt.Printf(`{"type":"assistant","session_id":%q,"message":{"content":[{"type":"tool_use","name":"Read","input":{}}]}}`+"\n", sessionID)
		fmt.Printf(`{"type":"result","subtype":"success","is_error":false,"session_id":%q}`+"\n", sessionID)
	case "codex":
		fmt.Printf(`{"type":"thread.started","thread_id":%q}`+"\n", sessionID)
		fmt.Println(`{"type":"item.completed","item":{"type":"tool_call","name":"Read"}}`)
		fmt.Println(`{"type":"turn.completed"}`)
	}
	return true
}

func wave4ContaminationTurn(provider string) bool {
	if os.Getenv("W4_ADAPTER_CONTAMINATION") != "1" {
		return false
	}
	foreign := provider + "-foreign-session"
	switch provider {
	case "cursor":
		fmt.Printf(`{"type":"system","subtype":"init","session_id":%q}`+"\n", foreign)
		fmt.Printf(`{"type":"result","subtype":"success","is_error":false,"result":%q}`+"\n", "FOREIGN_SESSION")
	case "claude":
		fmt.Printf(`{"type":"system","subtype":"init","session_id":%q}`+"\n", foreign)
		fmt.Printf(`{"type":"assistant","session_id":%q,"message":{"content":[{"type":"text","text":"FOREIGN_SESSION"}]}}`+"\n", foreign)
		fmt.Printf(`{"type":"result","subtype":"success","is_error":false,"result":"FOREIGN_SESSION","session_id":%q}`+"\n", foreign)
	case "codex":
		fmt.Printf(`{"type":"thread.started","thread_id":%q}`+"\n", foreign)
		fmt.Printf(`{"type":"item.completed","item":{"type":"agent_message","text":"FOREIGN_SESSION"}}` + "\n")
		fmt.Println(`{"type":"turn.completed"}`)
	}
	return true
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
