package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"agent-workflow-toolkit/internal/gateway"
	protocol "agent-workflow-toolkit/internal/protocol/v1"
)

func TestGatewayCLIExecutesRequestAndPersistsState(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	binary := filepath.Join(root, "agent-workflow-core")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}

	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = mustWorkingDirectory(t)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}

	create, err := protocol.NewRequest("cli-create-task", "createTask", gateway.CreateTaskInput{
		Title:     "CLI smoke",
		Objective: "exercise the gateway executable",
		Workspace: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	created := runGatewayCLI(t, binary, stateDir, create)
	if !created.OK {
		t.Fatalf("createTask CLI response = %#v", created.Error)
	}
	var result gateway.CreateTaskResult
	if err := json.Unmarshal(created.Payload, &result); err != nil {
		t.Fatalf("decode createTask payload: %v", err)
	}
	if result.Task.ID == "" || result.Revision.ID == "" {
		t.Fatalf("createTask payload did not contain task/revision: %#v", result)
	}

	snapshotPath := filepath.Join(stateDir, "tasks", result.Task.ID, "snapshot.json")
	eventsPath := filepath.Join(stateDir, "tasks", result.Task.ID, "events.jsonl")
	if raw, err := os.ReadFile(snapshotPath); err != nil || !json.Valid(raw) {
		t.Fatalf("CLI snapshot is missing or invalid: path=%s err=%v", snapshotPath, err)
	}
	if raw, err := os.ReadFile(eventsPath); err != nil || !strings.Contains(string(raw), "task.created") {
		t.Fatalf("CLI event log is missing task.created: path=%s err=%v", eventsPath, err)
	}

	missingNode, err := protocol.NewRequest("cli-missing-node", "runPreflight", gateway.RunPreflightInput{NodeID: "missing-node"})
	if err != nil {
		t.Fatal(err)
	}
	failed := runGatewayCLI(t, binary, stateDir, missingNode)
	if failed.OK || failed.Error == nil || failed.Error.Code != "unknown_node" {
		t.Fatalf("unknown node CLI response = %#v", failed)
	}
}

func TestCLIControlUsesIndependentCoordinationDirectory(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "business-state")
	coordDir := filepath.Join(root, "machine-coordination")
	binary := filepath.Join(root, "agent-workflow-core")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = mustWorkingDirectory(t)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	for _, args := range [][]string{{"--status", "--coordination-dir", coordDir}, {"--enable", "--coordination-dir", coordDir}} {
		command := exec.Command(binary, args...)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("control command %v: %v\n%s", args, err, output)
		}
	}
	if _, err := os.Stat(filepath.Join(coordDir, "coordination.json")); err != nil {
		t.Fatalf("coordination state was not written to independent root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "coordination.json")); !os.IsNotExist(err) {
		t.Fatalf("business state unexpectedly contains coordination state, err=%v", err)
	}

	command := exec.Command(binary, "--disable", "--coordination-dir", coordDir)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("disable command: %v\n%s", err, output)
	}
	var result struct {
		FrameworkEnabled bool   `json:"framework_enabled"`
		GateEpoch        uint64 `json:"gate_epoch"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode disable output: %v; output=%s", err, output)
	}
	if result.FrameworkEnabled || result.GateEpoch != 2 {
		t.Fatalf("disable output = %#v", result)
	}
}

func mustWorkingDirectory(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return directory
}

func runGatewayCLI(t *testing.T, binary, stateDir string, request protocol.Request) protocol.Response {
	t.Helper()
	input, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "--gateway", "--state-dir", stateDir)
	command.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("run CLI: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	response, err := protocol.DecodeResponse(bytes.NewReader(stdout.Bytes()))
	if err != nil {
		t.Fatalf("decode CLI response: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if response.RequestID != request.RequestID {
		t.Fatalf("CLI response request id = %q, want %q", response.RequestID, request.RequestID)
	}
	if stderr.Len() != 0 {
		t.Fatalf("CLI wrote unexpected stderr: %s", fmt.Sprint(stderr.String()))
	}
	return response
}
