package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agent-workflow-toolkit/internal/gateway"
	"agent-workflow-toolkit/internal/orchestration"
	protocol "agent-workflow-toolkit/internal/protocol/v1"
)

// CoreClient is the public bridge's narrow client for the versioned Gateway
// process. It intentionally invokes the core command instead of reading task
// snapshots directly, so action issuance, request journaling and coordination
// locks remain owned by the core.
type CoreClient struct {
	Executable      string
	StateDir        string
	PolicyFile      string
	RolesManifest   string
	CoordinationDir string
	LockTimeout     time.Duration
}

func (c CoreClient) Call(ctx context.Context, operation string, input any, output any) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if strings.TrimSpace(c.StateDir) == "" {
		return errors.New("state-dir is required")
	}
	request, err := protocol.NewRequest(orchestration.NewID("bridge-request"), operation, input)
	if err != nil {
		return err
	}
	rawRequest, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode core request: %w", err)
	}
	command, err := c.command(ctx)
	if err != nil {
		return err
	}
	command.Stdin = bytes.NewReader(append(rawRequest, '\n'))
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		// Provider output is never part of this error. The core process may have
		// emitted diagnostics to stderr, but the bridge does not persist or
		// relay raw provider streams.
		return fmt.Errorf("core gateway invocation failed: %w", err)
	}
	response, err := protocol.DecodeResponse(bytes.NewReader(stdout.Bytes()))
	if err != nil {
		return fmt.Errorf("decode core response: %w", err)
	}
	if !response.OK {
		if response.Error == nil {
			return errors.New("core gateway returned an invalid error response")
		}
		return &gateway.Error{Code: response.Error.Code, Message: response.Error.Message, Details: response.Error.Details}
	}
	if output == nil {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(response.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode core payload: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("core payload contains trailing JSON")
		}
		return fmt.Errorf("decode core payload trailing data: %w", err)
	}
	return nil
}

func (c CoreClient) PollHostActions(ctx context.Context, input gateway.PollHostActionsInput) (gateway.HostActionBatch, error) {
	var output gateway.HostActionBatch
	err := c.Call(ctx, "pollHostActions", input, &output)
	return output, err
}

func (c CoreClient) GetAttempt(ctx context.Context, input gateway.GetAttemptInput) (gateway.GetAttemptResult, error) {
	var output gateway.GetAttemptResult
	err := c.Call(ctx, "getAttempt", input, &output)
	return output, err
}

func (c CoreClient) SubmitHostActionResult(ctx context.Context, input gateway.SubmitHostActionResultInput) (gateway.ActionReceipt, error) {
	var output gateway.ActionReceipt
	err := c.Call(ctx, "submitHostActionResult", input, &output)
	return output, err
}

func (c CoreClient) RequestCancel(ctx context.Context, input gateway.CancelInput) (gateway.CancelResult, error) {
	var output gateway.CancelResult
	err := c.Call(ctx, "requestCancel", input, &output)
	return output, err
}

func (c CoreClient) command(ctx context.Context) (*exec.Cmd, error) {
	executable := strings.TrimSpace(c.Executable)
	if executable == "" {
		executable = defaultCoreExecutable()
	}
	if strings.TrimSpace(executable) == "" {
		return nil, errors.New("core executable is required")
	}
	lockTimeout := c.LockTimeout
	if lockTimeout <= 0 {
		lockTimeout = 5 * time.Second
	}
	args := []string{"--gateway", "--state-dir", filepath.Clean(c.StateDir), "--lock-timeout", lockTimeout.String()}
	if strings.TrimSpace(c.PolicyFile) != "" {
		args = append(args, "--policy-file", filepath.Clean(c.PolicyFile))
	}
	if strings.TrimSpace(c.RolesManifest) != "" {
		args = append(args, "--roles-manifest", filepath.Clean(c.RolesManifest))
	}
	if strings.TrimSpace(c.CoordinationDir) != "" {
		args = append(args, "--coordination-dir", filepath.Clean(c.CoordinationDir))
	}
	return exec.CommandContext(ctx, executable, args...), nil
}

func defaultCoreExecutable() string {
	if executable, err := os.Executable(); err == nil {
		directory := filepath.Dir(executable)
		name := "agent-workflow-core"
		if strings.EqualFold(filepath.Ext(executable), ".exe") {
			name += ".exe"
		}
		candidate := filepath.Join(directory, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "agent-workflow-core"
}
