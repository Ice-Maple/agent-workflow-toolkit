package bridge

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"agent-workflow-toolkit/internal/orchestration"
)

func TestNewRuntimeRejectsFrozenContractMismatchesBeforeProviderCreation(t *testing.T) {
	base := validBridgeRequest(t)
	tests := []struct {
		name   string
		mutate func(*Request)
		want   string
	}{
		{"profile hash", func(r *Request) { r.ExpectedProfileHash = strings.Repeat("0", 64) }, "adapter profile hash mismatch"},
		{"profile content", func(r *Request) { r.Profile.Executable = "changed" }, "adapter profile content hash mismatch"},
		{"attempt", func(r *Request) {
			r.Action.AttemptID = "attempt-other"
			r.Action.ParametersHash = orchestration.HashParameters(r.Action.Parameters)
		}, "core-issued action identity"},
		{"workspace", func(r *Request) {
			r.Action.Parameters["workspace"] = t.TempDir()
			r.Action.ParametersHash = orchestration.HashParameters(r.Action.Parameters)
		}, "workspace mismatch"},
		{"canary", func(r *Request) {
			r.Action.Parameters["task_canary"] = "wrong"
			r.Action.ParametersHash = orchestration.HashParameters(r.Action.Parameters)
		}, "task canary mismatch"},
		{"action parameters", func(r *Request) { r.Action.Parameters["workspace"] = "tampered" }, "parameters hash mismatch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := base
			request.Action.Parameters = cloneParameters(base.Action.Parameters)
			tt.mutate(&request)
			if _, err := NewRuntime(request); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewRuntime() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateOwnerForRequestRejectsStaleAndMismatchedOwner(t *testing.T) {
	request := validBridgeRequest(t)
	marker, err := currentProcessStartMarker()
	if err != nil {
		t.Fatal(err)
	}
	receipt := OwnerReceipt{
		SchemaVersion: OwnerReceiptSchema, AttemptID: request.AttemptID, ActionID: request.Action.ID,
		Provider: ProviderCursor, ProfileID: request.Profile.ID, ProfileHash: request.Profile.ProfileHash,
		Workspace: request.Attempt.Binding.Workspace, OwnerPID: os.Getpid(), OwnerStartMarker: marker,
		Endpoint: "127.0.0.1:4242", Capability: "private-capability", State: "running",
	}
	if err := validateOwnerForRequest(receipt, request); err != nil {
		t.Fatalf("valid owner rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*OwnerReceipt)
		want   string
	}{
		{"profile", func(r *OwnerReceipt) { r.ProfileHash = strings.Repeat("0", 64) }, "owner_mismatch"},
		{"workspace", func(r *OwnerReceipt) { r.Workspace = t.TempDir() }, "owner_mismatch"},
		{"endpoint", func(r *OwnerReceipt) { r.Endpoint = "192.0.2.1:4242" }, "not loopback"},
		{"process marker", func(r *OwnerReceipt) { r.OwnerStartMarker = "reused-pid-marker" }, "PID/start marker"},
		{"terminal", func(r *OwnerReceipt) { r.State = "terminal" }, "stale_owner"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := receipt
			tt.mutate(&candidate)
			if err := validateOwnerForRequest(candidate, request); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateOwnerForRequest() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestOwnerReceiptPublicViewAndPersistenceRedactSecretsAndProviderOutput(t *testing.T) {
	store, err := NewOwnerStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	receipt := OwnerReceipt{
		SchemaVersion: OwnerReceiptSchema, AttemptID: "attempt-1", ActionID: "action-1", Provider: ProviderCursor,
		ProfileID: "profile-1", ProfileHash: strings.Repeat("a", 64), Workspace: t.TempDir(), OwnerPID: 42,
		OwnerStartMarker: "marker", Endpoint: "127.0.0.1:4242", Capability: "secret-capability",
		ProviderHandle: "handle-1", ProviderSessionID: "session-1", State: "running", LastHeartbeat: time.Now().UTC(),
	}
	if err := store.Save(receipt); err != nil {
		t.Fatal(err)
	}
	loaded, found, err := store.Load(receipt.AttemptID)
	if err != nil || !found || loaded.Capability != receipt.Capability {
		t.Fatalf("Load() = (%#v, %v, %v)", loaded, found, err)
	}
	raw, err := json.Marshal(publicOwnerReceipt(loaded))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secret-capability", "dispatch_prompt", "authorization", "raw_stdout", "raw_stderr", "token"} {
		if strings.Contains(strings.ToLower(string(raw)), forbidden) {
			t.Fatalf("public receipt leaked %q: %s", forbidden, raw)
		}
	}
}

func TestCurrentProcessStartMarkerCanBeRevalidated(t *testing.T) {
	marker, err := currentProcessStartMarker()
	if err != nil {
		t.Fatal(err)
	}
	matched, err := processIdentityMatches(os.Getpid(), marker)
	if err != nil || !matched {
		t.Fatalf("processIdentityMatches() = (%v, %v)", matched, err)
	}
	matched, err = processIdentityMatches(os.Getpid(), marker+"-stale")
	if err != nil || matched {
		t.Fatalf("stale processIdentityMatches() = (%v, %v)", matched, err)
	}
}

func TestSendOwnerActionRejectsUnavailableOwnerWithoutRelaunch(t *testing.T) {
	request := validBridgeRequest(t)
	receipt := OwnerReceipt{
		SchemaVersion: OwnerReceiptSchema, AttemptID: request.AttemptID, ActionID: request.Action.ID,
		Provider: ProviderCursor, ProfileID: request.Profile.ID, ProfileHash: request.Profile.ProfileHash,
		Workspace: request.Attempt.Binding.Workspace, OwnerPID: 42, OwnerStartMarker: "marker",
		Endpoint: "127.0.0.1:1", Capability: "private-capability", State: "running",
	}
	if _, err := sendOwnerAction(t.Context(), receipt, request.Action); err == nil || !strings.Contains(err.Error(), "stale_owner") {
		t.Fatalf("sendOwnerAction() error = %v", err)
	}
}

func validBridgeRequest(t *testing.T) Request {
	t.Helper()
	workspace := t.TempDir()
	profile := Profile{AdapterProfile: orchestration.AdapterProfile{ID: "profile-1", Adapter: ProviderCursor, Executable: "cursor", Trust: "authorized", Mode: "plan"}, TrustAuthorized: true}
	profile.ProfileHash = orchestration.HashAdapterProfile(profile.AdapterProfile)
	attempt := orchestration.Attempt{ID: "attempt-1", TaskID: "task-1", NodeID: "node-1", RevisionID: "revision-1", Status: orchestration.AttemptPrepared, Binding: orchestration.Binding{Role: "reviewer", Adapter: ProviderCursor, ProfileID: profile.ID, Model: "configured-model", Reasoning: "high", Permission: "read-only", Workspace: workspace}, TaskCanary: "canary-1", MCPIsolation: "strict"}
	prompt := "review the frozen task"
	action := orchestration.HostAction{ID: "action-1", AttemptID: attempt.ID, Kind: orchestration.ActionLaunch, Sequence: 1, Lease: "lease-1", Status: orchestration.ActionPending, Parameters: map[string]any{"workspace": workspace, "task_canary": attempt.TaskCanary, "dispatch_prompt": prompt, "dispatch_prompt_hash": hashText(prompt), "adapter_profile_id": profile.ID, "adapter_profile_hash": profile.ProfileHash}, IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Minute).UTC()}
	action.ParametersHash = orchestration.HashParameters(action.Parameters)
	return Request{SchemaVersion: BridgeSchema, AttemptID: attempt.ID, ControllerInstanceID: "controller-1", Action: action, Attempt: attempt, Profile: profile, ExpectedProfileHash: profile.ProfileHash}
}

func cloneParameters(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
