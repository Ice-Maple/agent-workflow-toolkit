package codexcli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInvocationBuildsSafeDurableOutputCommand(t *testing.T) {
	i := Invocation{
		Executable:            "codex.exe",
		Workspace:             `E:\projects\agent-workflow-toolkit`,
		Model:                 "gpt-5.6-luna",
		OutputLastMessagePath: `E:\temp\last-message.txt`,
		Ephemeral:             true,
	}
	args, err := i.Args("return the smoke token")
	if err != nil {
		t.Fatalf("Args() error = %v", err)
	}
	want := []string{
		"--ask-for-approval", "untrusted",
		"--sandbox", "read-only",
		"--cd", `E:\projects\agent-workflow-toolkit`,
		"--model", "gpt-5.6-luna",
		"exec", "--json",
		"--output-last-message", `E:\temp\last-message.txt`,
		"--ephemeral", "return the smoke token",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("Args() = %#v, want %#v", args, want)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--force") || strings.Contains(joined, "--dangerously-bypass") {
		t.Fatalf("unsafe bypass flag found in args: %s", joined)
	}
}

func TestNormalizeRecoversDurableMessageWhenStdoutIsEmpty(t *testing.T) {
	result := (RawResult{ExitCode: 0, LastMessage: []byte("\nADAPTER_SMOKE_OK\n")}).Normalize()
	if result.Status != StatusCompleted || result.Reason != "durable_last_message" || result.FinalText != "ADAPTER_SMOKE_OK" {
		t.Fatalf("unexpected durable result: %#v", result)
	}
}

func TestNormalizeMarksEmptySuccessfulTurnAsDeliveryMissing(t *testing.T) {
	result := (RawResult{ExitCode: 0, Stdout: []byte{}}).Normalize()
	if result.Status != StatusDeliveryMissing || result.Reason != "delivery_missing" {
		t.Fatalf("unexpected empty result: %#v", result)
	}
}

func TestNormalizeDoesNotTreatToolOnlyJSONLAsFinal(t *testing.T) {
	stdout := []byte(`{"type":"item.completed","item":{"type":"tool_call","name":"CreatePlan","input":{"text":"report"}}}` + "\n" + `{"type":"turn.completed","status":"success"}`)
	result := (RawResult{ExitCode: 0, Stdout: stdout}).Normalize()
	if result.Status != StatusDeliveryMissing || result.Reason != "delivery_missing" {
		t.Fatalf("tool-only JSONL was treated as final: %#v", result)
	}
}

func TestNormalizeExtractsStructuredAgentMessage(t *testing.T) {
	stdout := []byte(`{"type":"item.completed","item":{"type":"agent_message","text":"ADAPTER_SMOKE_OK"}}`)
	result := (RawResult{ExitCode: 0, Stdout: stdout}).Normalize()
	if result.Status != StatusCompleted || result.Source != "item.completed" || result.FinalText != "ADAPTER_SMOKE_OK" {
		t.Fatalf("unexpected structured result: %#v", result)
	}
}

func TestRunUsesDurableOutputFileWithLocalHelperProcess(t *testing.T) {
	lastMessagePath := filepath.Join(t.TempDir(), "last-message.txt")
	i := Invocation{
		Executable:            os.Args[0],
		Workspace:             `E:\projects\agent-workflow-toolkit`,
		Model:                 "fixture-model",
		OutputLastMessagePath: lastMessagePath,
		Environment: []string{
			"CODEX_ADAPTER_HELPER=1",
			"CODEX_ADAPTER_LAST_MESSAGE=" + lastMessagePath,
		},
	}
	result, err := i.Run(t.Context(), "return the smoke token")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != StatusCompleted || result.Reason != "durable_last_message" || result.FinalText != "ADAPTER_SMOKE_OK" {
		t.Fatalf("unexpected helper result: %#v", result)
	}
}

func TestRunReturnsStartErrorForMissingExecutable(t *testing.T) {
	lastMessagePath := filepath.Join(t.TempDir(), "last-message.txt")
	_, err := (Invocation{
		Executable:            filepath.Join(t.TempDir(), "missing-codex.exe"),
		Workspace:             `E:\projects\agent-workflow-toolkit`,
		Model:                 "fixture-model",
		OutputLastMessagePath: lastMessagePath,
	}).Run(t.Context(), "return the smoke token")
	if err == nil {
		t.Fatal("Run() accepted a missing executable as a provider result")
	}
}
