package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWave4StreamReplayMatrix(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	fixtureRoot := filepath.Join(filepath.Dir(source), "..", "..", "..", "tests", "orchestration", "fixtures", "wave4", "replay")
	cases := []struct {
		name           string
		file           string
		expectedID     string
		expectedText   string
		expectedSource string
		terminal       bool
		complete       bool
	}{
		{name: "cursor-success", file: "cursor-success.jsonl", expectedID: "cursor-replay-1", expectedText: "ADAPTER_SMOKE_OK", expectedSource: "assistant.text", terminal: true, complete: true},
		{name: "claude-success", file: "claude-success.jsonl", expectedID: "claude-replay-1", expectedText: "ADAPTER_SMOKE_OK", expectedSource: "result.result", terminal: true, complete: true},
		{name: "codex-success", file: "codex-success.jsonl", expectedID: "codex-replay-1", expectedText: "ADAPTER_SMOKE_OK", expectedSource: "item.completed.agent_message", terminal: true, complete: true},
		{name: "tool-only", file: "tool-only.jsonl", expectedID: "tool-only-replay-1", terminal: true, complete: false},
		{name: "malformed-diagnostic", file: "malformed-diagnostic.jsonl", terminal: true, complete: false},
		{name: "provider-error", file: "provider-error.jsonl", expectedID: "error-replay-1", terminal: true, complete: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(fixtureRoot, testCase.file))
			if err != nil {
				t.Fatalf("read replay fixture: %v", err)
			}
			summary := ParseJSONL(raw)
			if summary.SessionID != testCase.expectedID {
				t.Fatalf("session ID = %q, want %q", summary.SessionID, testCase.expectedID)
			}
			if summary.FinalText != testCase.expectedText {
				t.Fatalf("final text = %q, want %q", summary.FinalText, testCase.expectedText)
			}
			if summary.FinalSource != testCase.expectedSource {
				t.Fatalf("final source = %q, want %q", summary.FinalSource, testCase.expectedSource)
			}
			if summary.Terminal != testCase.terminal {
				t.Fatalf("terminal = %t, want %t", summary.Terminal, testCase.terminal)
			}
			if summary.DeliveryComplete() != testCase.complete {
				t.Fatalf("delivery complete = %t, want %t; summary=%#v", summary.DeliveryComplete(), testCase.complete, summary)
			}
		})
	}
}
