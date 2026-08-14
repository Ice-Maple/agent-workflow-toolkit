package bridge

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	"agent-workflow-toolkit/internal/gateway"
	"agent-workflow-toolkit/internal/orchestration"
)

const (
	ownerStateDirectory = "owners"
	ownerPollInterval   = 250 * time.Millisecond
	ownerHeartbeat      = 5 * time.Second
)

type OwnerStore struct {
	root string
	mu   sync.Mutex
}

func NewOwnerStore(root string) (*OwnerStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("bridge state directory is required")
	}
	root = filepath.Clean(root)
	if err := os.MkdirAll(filepath.Join(root, ownerStateDirectory), 0o700); err != nil {
		return nil, fmt.Errorf("create bridge state directory: %w", err)
	}
	return &OwnerStore{root: root}, nil
}

func (s *OwnerStore) receiptPath(attemptID string) (string, error) {
	if s == nil || strings.TrimSpace(s.root) == "" {
		return "", errors.New("bridge owner store is unavailable")
	}
	if strings.TrimSpace(attemptID) == "" || strings.ContainsAny(attemptID, `/\\`) {
		return "", errors.New("invalid attempt id")
	}
	return filepath.Join(s.root, ownerStateDirectory, hashToken(attemptID)+".json"), nil
}

func (s *OwnerStore) Load(attemptID string) (OwnerReceipt, bool, error) {
	path, err := s.receiptPath(attemptID)
	if err != nil {
		return OwnerReceipt{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var raw []byte
	for attempt := 0; attempt < 20; attempt++ {
		raw, err = os.ReadFile(path)
		if err == nil {
			break
		}
		if errors.Is(err, os.ErrNotExist) {
			return OwnerReceipt{}, false, nil
		}
		if runtime.GOOS != "windows" {
			return OwnerReceipt{}, false, err
		}
		// Windows can briefly reject a reader while the owner atomically
		// replaces its receipt. A bounded retry keeps cross-process polling
		// from turning that normal publication window into a stale owner.
		time.Sleep(5 * time.Millisecond)
	}
	if err != nil {
		return OwnerReceipt{}, false, err
	}
	var receipt OwnerReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return OwnerReceipt{}, false, fmt.Errorf("decode owner receipt: %w", err)
	}
	if err := validateOwnerReceipt(receipt); err != nil {
		return OwnerReceipt{}, false, err
	}
	return receipt, true, nil
}

func (s *OwnerStore) Save(receipt OwnerReceipt) error {
	if err := validateOwnerReceipt(receipt); err != nil {
		return err
	}
	path, err := s.receiptPath(receipt.AttemptID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	receipt.LastHeartbeat = time.Now().UTC()
	raw, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return fmt.Errorf("encode owner receipt: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".owner-*.tmp")
	if err != nil {
		return fmt.Errorf("create owner receipt temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("protect owner receipt temp: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write owner receipt temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close owner receipt temp: %w", err)
	}
	if err := replacePrivateFile(tmpName, path); err != nil {
		return fmt.Errorf("replace owner receipt: %w", err)
	}
	return nil
}

func replacePrivateFile(tempPath, targetPath string) error {
	if err := os.Rename(tempPath, targetPath); err == nil {
		return nil
	}
	backupPath := targetPath + ".backup"
	_ = os.Remove(backupPath)
	if err := os.Rename(targetPath, backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		_ = os.Rename(backupPath, targetPath)
		return err
	}
	_ = os.Remove(backupPath)
	return nil
}

func (s *OwnerStore) Remove(attemptID string) error {
	path, err := s.receiptPath(attemptID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func validateOwnerReceipt(receipt OwnerReceipt) error {
	if receipt.SchemaVersion != OwnerReceiptSchema {
		return fmt.Errorf("unsupported owner receipt schema %q", receipt.SchemaVersion)
	}
	if strings.TrimSpace(receipt.AttemptID) == "" || strings.TrimSpace(receipt.ActionID) == "" || strings.TrimSpace(receipt.Provider) == "" || strings.TrimSpace(receipt.ProfileID) == "" || strings.TrimSpace(receipt.ProfileHash) == "" {
		return errors.New("owner receipt identity is incomplete")
	}
	if strings.TrimSpace(receipt.Workspace) == "" || receipt.OwnerPID <= 0 || strings.TrimSpace(receipt.OwnerStartMarker) == "" || strings.TrimSpace(receipt.Endpoint) == "" || strings.TrimSpace(receipt.Capability) == "" {
		return errors.New("owner receipt owner identity is incomplete")
	}
	if receipt.State == "" {
		return errors.New("owner receipt state is required")
	}
	return nil
}

func newCapability() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func hashToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func OwnerCapabilityHash(receipt OwnerReceipt) string {
	return hashToken(receipt.Capability)
}

type ownerIPCRequest struct {
	SchemaVersion    string                   `json:"schema_version"`
	Capability       string                   `json:"capability"`
	OwnerStartMarker string                   `json:"owner_start_marker"`
	AttemptID        string                   `json:"attempt_id"`
	Operation        string                   `json:"operation"`
	Action           orchestration.HostAction `json:"action"`
}

type ownerIPCResponse struct {
	OK     bool                            `json:"ok"`
	Result *orchestration.HostActionResult `json:"result,omitempty"`
	Error  string                          `json:"error,omitempty"`
}

type ownerServer struct {
	runtime     *Runtime
	core        CoreClient
	store       *OwnerStore
	receipt     OwnerReceipt
	listener    net.Listener
	marker      string
	mu          sync.Mutex
	executionMu sync.Mutex
	results     map[string]orchestration.HostActionResult
	actions     map[string]orchestration.HostAction
	done        chan struct{}
	doneOnce    sync.Once
}

func (s *ownerServer) finish() {
	s.doneOnce.Do(func() {
		close(s.done)
		_ = s.listener.Close()
	})
}

func (s *ownerServer) serve(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-s.done:
			return nil
		default:
		}
		_ = s.listener.(*net.TCPListener).SetDeadline(time.Now().Add(ownerPollInterval))
		connection, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return nil
			default:
			}
			if timeoutError, ok := err.(net.Error); ok && timeoutError.Timeout() {
				continue
			}
			return err
		}
		go s.handleConnection(ctx, connection)
	}
}

func (s *ownerServer) handleConnection(ctx context.Context, connection net.Conn) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
	var request ownerIPCRequest
	if err := json.NewDecoder(bufio.NewReader(connection)).Decode(&request); err != nil {
		_ = json.NewEncoder(connection).Encode(ownerIPCResponse{Error: "invalid owner request"})
		return
	}
	if request.SchemaVersion != BridgeSchema || request.Capability != s.receipt.Capability || request.OwnerStartMarker != s.marker || request.AttemptID != s.receipt.AttemptID || request.Operation != "execute" {
		_ = json.NewEncoder(connection).Encode(ownerIPCResponse{Error: "stale_owner"})
		return
	}
	result, err := s.execute(ctx, request.Action)
	response := ownerIPCResponse{OK: err == nil, Result: &result}
	if err != nil {
		response.Result = nil
		response.Error = err.Error()
	}
	_ = json.NewEncoder(connection).Encode(response)
	s.mu.Lock()
	terminal := s.receipt.State == "terminal" || s.receipt.State == "cleanup_pending"
	s.mu.Unlock()
	if err == nil && response.Result != nil && (response.Result.Terminal || terminal) {
		s.finish()
	}
}

func (s *ownerServer) execute(ctx context.Context, action orchestration.HostAction) (orchestration.HostActionResult, error) {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	if s.results == nil {
		s.results = map[string]orchestration.HostActionResult{}
	}
	if s.actions == nil {
		s.actions = map[string]orchestration.HostAction{}
	}
	if previous, ok := s.results[action.ID]; ok {
		if !reflect.DeepEqual(s.actions[action.ID], action) {
			return orchestration.HostActionResult{}, errors.New("stale_owner: action identity mismatch")
		}
		if _, err := s.core.SubmitHostActionResult(ctx, gateway.SubmitHostActionResultInput{AttemptID: action.AttemptID, ActionID: action.ID, Lease: action.Lease, Result: previous}); err != nil {
			return previous, err
		}
		return previous, nil
	}
	s.mu.Lock()
	if action.AttemptID != s.receipt.AttemptID {
		s.mu.Unlock()
		return orchestration.HostActionResult{}, errors.New("stale_owner: attempt mismatch")
	}
	s.mu.Unlock()
	s.actions[action.ID] = action
	result, executionErr := s.runtime.Execute(ctx, action)
	if executionErr != nil && strings.TrimSpace(result.Status) == "" {
		return result, executionErr
	}
	if _, err := s.core.SubmitHostActionResult(ctx, gateway.SubmitHostActionResultInput{AttemptID: action.AttemptID, ActionID: action.ID, Lease: action.Lease, Result: result}); err != nil {
		s.mu.Lock()
		s.receipt.State = "outcome_unknown"
		receipt := s.receipt
		s.mu.Unlock()
		_ = s.store.Save(receipt)
		return result, err
	}
	s.results[action.ID] = result
	s.mu.Lock()
	s.receipt.LastActionID = action.ID
	s.receipt.LastActionSequence = action.Sequence
	s.receipt.ProviderHandle = result.ProviderHandle
	s.receipt.ProviderSessionID = result.ProviderSessionID
	s.receipt.State = "running"
	if result.Terminal || action.Kind == orchestration.ActionCancelHost || result.Status == "failed" || result.Status == "error" || result.Status == "timed_out" || result.Status == "timeout" {
		s.receipt.State = "terminal"
	}
	receipt := s.receipt
	s.mu.Unlock()
	if err := s.store.Save(receipt); err != nil {
		return result, err
	}
	if receipt.State == "terminal" {
		if cleanupErr := s.runtime.Cleanup(); cleanupErr != nil {
			s.mu.Lock()
			s.receipt.State = "cleanup_pending"
			receipt = s.receipt
			s.mu.Unlock()
			_ = s.store.Save(receipt)
			return result, cleanupErr
		}
	}
	return result, nil
}

func sendOwnerAction(ctx context.Context, receipt OwnerReceipt, action orchestration.HostAction) (orchestration.HostActionResult, error) {
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", receipt.Endpoint)
	if err != nil {
		return orchestration.HostActionResult{}, errors.New("stale_owner: owner endpoint is unavailable")
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return orchestration.HostActionResult{}, errors.New("outcome_unknown: owner connection deadline could not be established")
	}
	if err := json.NewEncoder(connection).Encode(ownerIPCRequest{SchemaVersion: BridgeSchema, Capability: receipt.Capability, OwnerStartMarker: receipt.OwnerStartMarker, AttemptID: receipt.AttemptID, Operation: "execute", Action: action}); err != nil {
		return orchestration.HostActionResult{}, errors.New("outcome_unknown: owner request delivery is uncertain")
	}
	var response ownerIPCResponse
	if err := json.NewDecoder(connection).Decode(&response); err != nil {
		return orchestration.HostActionResult{}, errors.New("outcome_unknown: owner response unavailable after request delivery")
	}
	if !response.OK || response.Result == nil {
		if response.Error == "" {
			response.Error = "stale_owner"
		}
		return orchestration.HostActionResult{}, errors.New(response.Error)
	}
	return *response.Result, nil
}

func SendOwnerActionForConvergence(ctx context.Context, receipt OwnerReceipt, action orchestration.HostAction) (orchestration.HostActionResult, error) {
	return sendOwnerAction(ctx, receipt, action)
}
