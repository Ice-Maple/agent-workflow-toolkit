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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	DefaultSandbox  = "read-only"
	DefaultApproval = "untrusted"
)

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
	Sandbox               string
	Approval              string
	OutputLastMessagePath string
	Ephemeral             bool
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
		"exec",
		"--json",
		"--output-last-message", i.OutputLastMessagePath,
	}
	if i.Ephemeral {
		args = append(args, "--ephemeral")
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
	args, err := i.Args(prompt)
	if err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(ctx, i.Executable, args...)
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
