package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agent-workflow-toolkit/internal/orchestration"
)

const ownerStartupTimeout = 15 * time.Second

type ownerBootstrap struct {
	Request              Request    `json:"request"`
	Core                 CoreClient `json:"core"`
	BridgeStateDir       string     `json:"bridge_state_dir"`
	ControllerInstanceID string     `json:"controller_instance_id"`
}

func startOwnerProcess(ctx context.Context, executable string, bootstrap ownerBootstrap, store *OwnerStore) (OwnerReceipt, error) {
	if ctx == nil {
		return OwnerReceipt{}, errors.New("context is required")
	}
	if store == nil {
		return OwnerReceipt{}, errors.New("owner store is required")
	}
	if strings.TrimSpace(executable) == "" {
		return OwnerReceipt{}, errors.New("bridge executable is required")
	}
	path, err := store.receiptPath(bootstrap.Request.AttemptID)
	if err != nil {
		return OwnerReceipt{}, err
	}
	bootstrapPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".bootstrap.json"
	raw, err := json.Marshal(bootstrap)
	if err != nil {
		return OwnerReceipt{}, fmt.Errorf("encode owner bootstrap: %w", err)
	}
	if err := writePrivateFile(bootstrapPath, raw); err != nil {
		return OwnerReceipt{}, err
	}
	command := exec.Command(executable, "--owner", "--owner-bootstrap", bootstrapPath)
	if err := configureDetachedProcess(command); err != nil {
		return OwnerReceipt{}, err
	}
	if err := command.Start(); err != nil {
		return OwnerReceipt{}, fmt.Errorf("start bridge owner: %w", err)
	}
	deadline := time.NewTimer(ownerStartupTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		receipt, found, loadErr := store.Load(bootstrap.Request.AttemptID)
		if loadErr != nil {
			return OwnerReceipt{}, loadErr
		}
		if found && receipt.State != "starting" {
			_ = os.Remove(bootstrapPath)
			return receipt, nil
		}
		select {
		case <-ctx.Done():
			return OwnerReceipt{}, ctx.Err()
		case <-deadline.C:
			if !found {
				_ = command.Process.Kill()
				_ = os.Remove(bootstrapPath)
				return OwnerReceipt{}, errors.New("bridge owner did not publish a receipt")
			}
			_ = os.Remove(bootstrapPath)
			return receipt, errors.New("bridge owner startup outcome is unknown")
		case <-ticker.C:
		}
	}
}

func runOwnerProcess(bootstrapPath string) error {
	bootstrapRaw, err := os.ReadFile(bootstrapPath)
	if err != nil {
		return fmt.Errorf("read owner bootstrap: %w", err)
	}
	_ = os.Remove(bootstrapPath)
	var bootstrap ownerBootstrap
	decoder := json.NewDecoder(strings.NewReader(string(bootstrapRaw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bootstrap); err != nil {
		return fmt.Errorf("decode owner bootstrap: %w", err)
	}
	if strings.TrimSpace(bootstrap.Request.AttemptID) == "" || bootstrap.Request.Action.Kind != orchestration.ActionLaunch {
		return errors.New("owner bootstrap must contain a launch Attempt action")
	}
	store, err := NewOwnerStore(bootstrap.BridgeStateDir)
	if err != nil {
		return err
	}
	if _, found, err := store.Load(bootstrap.Request.AttemptID); err != nil {
		return err
	} else if found {
		return errors.New("owner receipt already exists; refusing to replace it")
	}
	runtime, err := NewRuntime(bootstrap.Request)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen for bridge owner: %w", err)
	}
	capability, err := newCapability()
	if err != nil {
		_ = listener.Close()
		return err
	}
	marker, err := newCapability()
	if err != nil {
		_ = listener.Close()
		return err
	}
	receipt := OwnerReceipt{
		SchemaVersion:    OwnerReceiptSchema,
		AttemptID:        bootstrap.Request.AttemptID,
		ActionID:         bootstrap.Request.Action.ID,
		Provider:         normalizeProvider(bootstrap.Request.Attempt.Binding.Adapter),
		ProfileID:        bootstrap.Request.Profile.ID,
		ProfileHash:      bootstrap.Request.Profile.ProfileHash,
		Workspace:        bootstrap.Request.Attempt.Binding.Workspace,
		OwnerPID:         os.Getpid(),
		OwnerStartMarker: marker,
		Endpoint:         listener.Addr().String(),
		Capability:       capability,
		State:            "starting",
		LastHeartbeat:    time.Now().UTC(),
	}
	if err := store.Save(receipt); err != nil {
		_ = listener.Close()
		return err
	}
	server := &ownerServer{runtime: runtime, core: bootstrap.Core, store: store, receipt: receipt, listener: listener, marker: marker, done: make(chan struct{})}
	server.receipt = receipt
	result, executionErr := server.execute(context.Background(), bootstrap.Request.Action)
	if executionErr != nil && strings.TrimSpace(result.Status) == "" {
		server.mu.Lock()
		server.receipt.State = "failed"
		failedReceipt := server.receipt
		server.mu.Unlock()
		_ = store.Save(failedReceipt)
		_ = listener.Close()
		return executionErr
	}
	server.mu.Lock()
	state := server.receipt.State
	server.mu.Unlock()
	if state == "terminal" || state == "cleanup_pending" || state == "outcome_unknown" {
		_ = listener.Close()
		return nil
	}
	serveErr := server.serve(context.Background())
	_ = listener.Close()
	server.mu.Lock()
	state = server.receipt.State
	if serveErr != nil && state != "terminal" && state != "cleanup_pending" {
		server.receipt.State = "outcome_unknown"
		failedReceipt := server.receipt
		server.mu.Unlock()
		_ = store.Save(failedReceipt)
	} else {
		server.mu.Unlock()
	}
	return serveErr
}

func writePrivateFile(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create private bridge directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create private bridge file: %w", err)
	}
	if _, writeErr := file.Write(raw); writeErr != nil {
		_ = file.Close()
		return fmt.Errorf("write private bridge file: %w", writeErr)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close private bridge file: %w", err)
	}
	return nil
}
