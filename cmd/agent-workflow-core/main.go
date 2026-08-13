package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent-workflow-toolkit/internal/bridge"
	"agent-workflow-toolkit/internal/buildinfo"
	"agent-workflow-toolkit/internal/collaboration"
	"agent-workflow-toolkit/internal/coordination"
	"agent-workflow-toolkit/internal/gateway"
	"agent-workflow-toolkit/internal/orchestration"
	"agent-workflow-toolkit/internal/policy"
	protocol "agent-workflow-toolkit/internal/protocol/v1"
	"agent-workflow-toolkit/internal/roles"
	"agent-workflow-toolkit/internal/storage"
)

func main() {
	showVersion := flag.Bool("version", false, "print the core version")
	validateRequest := flag.Bool("validate-request", false, "validate one protocol v1 envelope from stdin")
	gatewayMode := flag.Bool("gateway", false, "handle one protocol v1 request through the TaskGateway")
	statusMode := flag.Bool("status", false, "show machine-local collaboration status")
	enableMode := flag.Bool("enable", false, "enable the machine-local collaboration framework gate")
	disableMode := flag.Bool("disable", false, "disable the machine-local collaboration framework gate")
	setWorkspaceMode := flag.Bool("set-workspace-mode", false, "set the mode for one canonical workspace")
	inspectCollaboration := flag.Bool("inspect-collaboration", false, "validate and print one business Skill collaboration.yaml")
	inspectRoles := flag.Bool("inspect-roles", false, "validate and print one Workflow/project roles.yaml registry")
	collaborationFile := flag.String("collaboration-file", "", "business Skill collaboration.yaml used by --inspect-collaboration")
	workspace := flag.String("workspace", "", "canonical workspace used by --status or --set-workspace-mode")
	workspaceMode := flag.String("workspace-mode", "", "workspace mode: framework, direct or single")
	stateDir := flag.String("state-dir", "", "directory for task state and request receipts")
	coordinationDir := flag.String("coordination-dir", "", "machine-local collaboration coordination directory")
	policyFile := flag.String("policy-file", "", "JSON policy file containing registered bindings and machine-local routes")
	rolesManifest := flag.String("roles-manifest", "", "Workflow/project roles.yaml to verify Role Prompt identities")
	lockTimeout := flag.Duration("lock-timeout", 5*time.Second, "maximum time to wait for exclusive state-dir access")
	flag.Parse()

	if *showVersion {
		fmt.Println(buildinfo.Format("agent-workflow-core", protocol.Major))
		return
	}
	if *inspectCollaboration || *inspectRoles {
		if *inspectCollaboration == *inspectRoles {
			fmt.Fprintln(os.Stderr, "exactly one of --inspect-collaboration and --inspect-roles is required")
			os.Exit(2)
		}
		var output any
		var err error
		if *inspectCollaboration {
			output, err = collaboration.Load(*collaborationFile)
		} else {
			output, err = roles.LoadManifest(*rolesManifest)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := encodeControlOutput(output); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	controlMode := *statusMode || *enableMode || *disableMode || *setWorkspaceMode
	if !*validateRequest && !*gatewayMode && !controlMode {
		fmt.Fprintln(os.Stderr, "usage: agent-workflow-core --version | --validate-request | --inspect-collaboration --collaboration-file FILE | --inspect-roles --roles-manifest FILE | --gateway [--state-dir DIR] [--coordination-dir DIR] [--policy-file FILE] [--roles-manifest FILE] | --status | --enable | --disable | --set-workspace-mode --workspace DIR --workspace-mode MODE")
		os.Exit(2)
	}

	if controlMode {
		selected := 0
		for _, selectedMode := range []bool{*statusMode, *enableMode, *disableMode, *setWorkspaceMode} {
			if selectedMode {
				selected++
			}
		}
		if selected != 1 {
			fmt.Fprintln(os.Stderr, "exactly one of --status, --enable, --disable and --set-workspace-mode is required")
			os.Exit(2)
		}
		root, err := resolveCoordinationRoot(*coordinationDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		manager, err := coordination.New(root, *lockTimeout)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		var output any
		switch {
		case *statusMode:
			output, err = manager.Status(*workspace)
		case *enableMode:
			output, err = manager.SetFrameworkEnabled(true)
		case *disableMode:
			output, err = disableAndConverge(manager, *lockTimeout)
		case *setWorkspaceMode:
			if strings.TrimSpace(*workspace) == "" || strings.TrimSpace(*workspaceMode) == "" {
				fmt.Fprintln(os.Stderr, "--set-workspace-mode requires --workspace and --workspace-mode")
				os.Exit(2)
			}
			output, err = manager.SetWorkspaceMode(*workspace, *workspaceMode)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := encodeControlOutput(output); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	request, err := protocol.DecodeRequest(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *validateRequest {
		writeSuccess(request.RequestID, map[string]any{
			"status":    "envelope_valid",
			"operation": request.Operation,
		})
		return
	}

	root := *stateDir
	if root == "" {
		configDir, configErr := os.UserConfigDir()
		if configErr != nil {
			fmt.Fprintln(os.Stderr, configErr)
			os.Exit(1)
		}
		root = filepath.Join(configDir, "agent-workflow-toolkit")
	}
	config := policy.DefaultConfig()
	if *policyFile != "" {
		raw, readErr := os.ReadFile(*policyFile)
		if readErr != nil {
			fmt.Fprintln(os.Stderr, readErr)
			os.Exit(1)
		}
		config, err = policy.LoadJSON(raw)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if strings.TrimSpace(*rolesManifest) != "" {
		registryConfig, loadErr := policy.LoadRoleManifest(*rolesManifest)
		if loadErr != nil {
			fmt.Fprintln(os.Stderr, loadErr)
			os.Exit(1)
		}
		config.RoleDefinitions = registryConfig.RoleDefinitions
		if validateErr := config.Validate(); validateErr != nil {
			fmt.Fprintln(os.Stderr, validateErr)
			os.Exit(1)
		}
	}
	collaborationRequest, stateDirForLock, runID, gateEpoch, attemptID, classifyErr := classifyCollaborationRequest(request)
	if classifyErr != nil {
		writeError(request.RequestID, "invalid_payload", classifyErr.Error(), nil)
		return
	}
	if collaborationRequest {
		coordRoot, coordErr := resolveCoordinationRoot(*coordinationDir)
		if coordErr != nil {
			fmt.Fprintln(os.Stderr, coordErr)
			os.Exit(1)
		}
		coordManager, coordErr := coordination.New(coordRoot, *lockTimeout)
		if coordErr != nil {
			fmt.Fprintln(os.Stderr, coordErr)
			os.Exit(1)
		}
		// The collaboration facade owns the complete lock order. It must not be
		// composed from the legacy state-dir-only one-shot path.
		var locks *coordination.LockSet
		var entry storage.ActiveRunEntry
		var lockErr error
		switch {
		case request.Operation == "createCollaborationRun":
			locks, lockErr = coordManager.AcquireRunLocks(stateDirForLock)
		case request.Operation == "prepareCollaborationInstance":
			locks, entry, lockErr = coordManager.AcquireRunLocksForRun(runID, gateEpoch)
		case request.Operation == "registerCollaborationOwner":
			locks, entry, lockErr = coordManager.AcquireRunLocksForRun(runID, gateEpoch)
		case request.Operation == "prepareAttempt":
			locks, entry, lockErr = coordManager.AcquireRunLocksForRun(runID, gateEpoch)
		case request.Operation == "recordCollaborationTechnicalRelease":
			locks, entry, lockErr = coordManager.AcquireRunLocksForConvergence(runID)
		case request.Operation == "requestCompletion":
			locks, entry, lockErr = coordManager.AcquireRunLocksForConvergence(runID)
		default:
			var found bool
			locks, entry, found, lockErr = coordManager.AcquireRunLocksForAttempt(attemptID)
			if lockErr == nil && !found {
				// There is no indexed collaboration Attempt. Retain the
				// state-dir-only compatibility path for ordinary protocol calls.
				runLegacyGateway(request, root, config, *lockTimeout)
				return
			}
		}
		if lockErr != nil {
			if errors.Is(lockErr, storage.ErrStateDirBusy) {
				writeError(request.RequestID, "state_dir_busy", "another gateway process owns this state-dir", nil)
				return
			}
			fmt.Fprintln(os.Stderr, lockErr)
			os.Exit(1)
		}
		if entry.StateDir != "" {
			stateDirForLock = entry.StateDir
		}
		if *stateDir != "" && stateDirForLock != "" && !samePath(*stateDir, stateDirForLock) {
			_ = locks.Release()
			writeError(request.RequestID, "state_dir_mismatch", "the request state-dir does not match the collaboration run", nil)
			return
		}
		if strings.TrimSpace(stateDirForLock) == "" {
			_ = locks.Release()
			writeError(request.RequestID, "state_dir_required", "collaboration request requires a state-dir", nil)
			return
		}
		root = stateDirForLock
		defer func() {
			if releaseErr := locks.Release(); releaseErr != nil {
				fmt.Fprintln(os.Stderr, releaseErr)
			}
		}()
		store, err := storage.New(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		core, err := gateway.NewWithCoordinationLocked(store, config, coordManager)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		payload, err := core.Handle(request)
		writeGatewayResult(request, payload, err)
		return
	}
	runLegacyGateway(request, root, config, *lockTimeout)
}

func disableAndConverge(manager *coordination.Manager, lockTimeout time.Duration) (coordination.DisableResult, error) {
	result, err := manager.Disable()
	if err != nil {
		return result, err
	}
	for index := range result.Runs {
		status := &result.Runs[index]
		entry, found, lookupErr := manager.LookupRun(status.RunID)
		if lookupErr != nil || !found {
			status.Status = "outcome_unknown"
			continue
		}
		attempts := entry.Attempts
		if len(attempts) == 0 && entry.AttemptID != "" {
			attempts = map[string]storage.ActiveAttemptEntry{entry.AttemptID: {AttemptID: entry.AttemptID, InstanceID: entry.InstanceID, OwnerPID: entry.OwnerPID, OwnerStartMarker: entry.OwnerStartMarker, OwnerEndpoint: entry.OwnerEndpoint, OwnerCapabilityHash: entry.OwnerCapabilityHash, OwnerStateDir: entry.OwnerStateDir}}
		}
		status.Status = "not_contacted"
		for attemptID, attemptEntry := range attempts {
			attemptStatus, convergenceErr := convergeDisabledAttempt(manager, entry, attemptEntry, attemptID, lockTimeout)
			if convergenceErr != nil && attemptStatus == "" {
				attemptStatus = "outcome_unknown"
			}
			attemptEntry.ShutdownStatus = attemptStatus
			entry.Attempts[attemptID] = attemptEntry
			if disableStatusPriority(attemptStatus) > disableStatusPriority(status.Status) {
				status.Status = attemptStatus
			}
		}
		locks, _, lockErr := manager.AcquireRunLocksForConvergence(entry.RunID)
		if lockErr != nil {
			status.Status = "outcome_unknown"
			return result, fmt.Errorf("persist disable convergence for run %s: %w", entry.RunID, lockErr)
		}
		latest, found, lookupErr := manager.GetRunUnderLock(entry.RunID)
		if lookupErr != nil || !found {
			_ = locks.Release()
			status.Status = "outcome_unknown"
			if lookupErr != nil {
				return result, fmt.Errorf("reload disable convergence for run %s: %w", entry.RunID, lookupErr)
			}
			return result, fmt.Errorf("reload disable convergence for run %s: active run is missing", entry.RunID)
		}
		latest.ShutdownStatus = status.Status
		for attemptID, attempted := range entry.Attempts {
			current := latest.Attempts[attemptID]
			current.ShutdownStatus = attempted.ShutdownStatus
			latest.Attempts[attemptID] = current
		}
		if updateErr := manager.UpdateRunUnderLock(latest); updateErr != nil {
			_ = locks.Release()
			status.Status = "outcome_unknown"
			return result, fmt.Errorf("write disable convergence for run %s: %w", entry.RunID, updateErr)
		}
		if releaseErr := locks.Release(); releaseErr != nil {
			status.Status = "outcome_unknown"
			return result, fmt.Errorf("release disable convergence locks for run %s: %w", entry.RunID, releaseErr)
		}
	}
	return result, nil
}

func convergeDisabledAttempt(manager *coordination.Manager, entry storage.ActiveRunEntry, attemptEntry storage.ActiveAttemptEntry, attemptID string, lockTimeout time.Duration) (string, error) {
	locks, _, found, err := manager.AcquireRunLocksForAttempt(attemptID)
	if err != nil || !found {
		return "outcome_unknown", err
	}
	store, err := storage.New(entry.StateDir)
	if err != nil {
		_ = locks.Release()
		return "outcome_unknown", err
	}
	core, err := gateway.NewWithCoordinationLocked(store, policy.DefaultConfig(), manager)
	if err != nil {
		_ = locks.Release()
		return "outcome_unknown", err
	}
	cancel, err := core.RequestCancel(gateway.CancelInput{AttemptID: attemptID, Reason: "framework disabled"})
	if err != nil {
		if gatewayErr, ok := err.(*gateway.Error); ok && gatewayErr.Code == "cancel_not_allowed" {
			attempt, getErr := core.GetAttempt(gateway.GetAttemptInput{AttemptID: attemptID})
			if getErr != nil {
				_ = locks.Release()
				return "outcome_unknown", getErr
			}
			switch shutdownStatusForAttempt(attempt.Attempt.Status) {
			case "cancelled":
				_ = locks.Release()
				return "cancelled", nil
			case "cancel_pending":
				_ = locks.Release()
				return "cancel_pending", nil
			case "contact_owner":
				batch, pollErr := core.PollHostActions(gateway.PollHostActionsInput{AttemptID: attemptID, ControllerInstanceID: "disable-convergence"})
				if pollErr != nil {
					_ = locks.Release()
					return "outcome_unknown", pollErr
				}
				for _, action := range batch.Actions {
					if action.Kind == orchestration.ActionCancelHost {
						cancel.Action = action
						goto cancelReady
					}
				}
				_ = locks.Release()
				return "cancel_pending", nil
			default:
				_ = locks.Release()
				return "outcome_unknown", nil
			}
		}
		_ = locks.Release()
		return "outcome_unknown", err
	}
cancelReady:
	if err := locks.Release(); err != nil {
		return "outcome_unknown", err
	}
	storeOwner, err := bridge.NewOwnerStore(attemptEntry.OwnerStateDir)
	if err != nil {
		return "not_contacted", err
	}
	receipt, found, err := storeOwner.Load(attemptID)
	if err != nil {
		return "not_contacted", err
	}
	if !found {
		return "not_contacted", nil
	}
	if bridge.OwnerCapabilityHash(receipt) != attemptEntry.OwnerCapabilityHash {
		return "stale_owner", nil
	}
	result, err := bridge.SendOwnerActionForConvergence(context.Background(), receipt, cancel.Action)
	if err != nil {
		return shutdownStatusForOwnerError(err), err
	}
	if result.Status == "cancelled" {
		return "cancelled", nil
	}
	return "cancel_pending", nil
}

func shutdownStatusForAttempt(status orchestration.AttemptStatus) string {
	switch status {
	case orchestration.AttemptCompleted, orchestration.AttemptFailed, orchestration.AttemptTimedOut, orchestration.AttemptCancelled:
		return "cancelled"
	case orchestration.AttemptCancelUnconfirmed:
		return "cancel_pending"
	case orchestration.AttemptCancelRequested:
		return "contact_owner"
	default:
		return "outcome_unknown"
	}
}

func shutdownStatusForOwnerError(err error) string {
	if err != nil && strings.HasPrefix(err.Error(), "stale_owner:") {
		return "stale_owner"
	}
	return "outcome_unknown"
}

func disableStatusPriority(status string) int {
	switch status {
	case "outcome_unknown":
		return 4
	case "stale_owner":
		return 3
	case "cancel_pending":
		return 2
	case "not_contacted":
		return 2
	case "cancelled":
		return 1
	default:
		return 0
	}
}

func runLegacyGateway(request protocol.Request, root string, config policy.Config, lockTimeout time.Duration) {
	rootLock, err := storage.AcquireRootLock(root, lockTimeout)
	if err != nil {
		if errors.Is(err, storage.ErrStateDirBusy) {
			writeError(request.RequestID, "state_dir_busy", "another gateway process owns this state-dir", nil)
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() {
		if releaseErr := rootLock.Release(); releaseErr != nil {
			fmt.Fprintln(os.Stderr, releaseErr)
		}
	}()
	store, err := storage.New(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	core, err := gateway.New(store, config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	payload, err := core.Handle(request)
	writeGatewayResult(request, payload, err)
}

func resolveCoordinationRoot(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return filepath.Clean(explicit), nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "agent-workflow-toolkit", "coordination"), nil
}

func classifyCollaborationRequest(request protocol.Request) (bool, string, string, uint64, string, error) {
	var raw struct {
		StateDir           string `json:"state_dir"`
		CollaborationRunID string `json:"collaboration_run_id"`
		GateEpoch          uint64 `json:"gate_epoch"`
		RunID              string `json:"run_id"`
		AttemptID          string `json:"attempt_id"`
	}
	if err := json.Unmarshal(request.Payload, &raw); err != nil {
		return false, "", "", 0, "", err
	}
	switch request.Operation {
	case "createCollaborationRun":
		return true, raw.StateDir, "", raw.GateEpoch, "", nil
	case "prepareAttempt":
		if raw.CollaborationRunID == "" {
			return false, "", "", 0, "", nil
		}
		return true, "", raw.CollaborationRunID, raw.GateEpoch, "", nil
	case "prepareCollaborationInstance":
		if raw.RunID == "" {
			return false, "", "", 0, "", nil
		}
		return true, "", raw.RunID, raw.GateEpoch, "", nil
	case "registerCollaborationOwner":
		if raw.RunID == "" {
			return false, "", "", 0, "", nil
		}
		return true, "", raw.RunID, raw.GateEpoch, "", nil
	case "recordCollaborationTechnicalRelease":
		return true, "", raw.RunID, 0, "", nil
	case "requestCompletion":
		if raw.CollaborationRunID == "" {
			return false, "", "", 0, "", nil
		}
		return true, "", raw.CollaborationRunID, raw.GateEpoch, raw.AttemptID, nil
	case "pollHostActions", "getAttempt", "submitHostActionResult", "bindProviderHandle", "appendEvent", "requestCancel":
		if raw.AttemptID == "" {
			return false, "", "", 0, "", nil
		}
		return true, "", "", 0, raw.AttemptID, nil
	default:
		return false, "", "", 0, "", nil
	}
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(filepath.Clean(left))
	rightAbs, rightErr := filepath.Abs(filepath.Clean(right))
	if leftErr != nil || rightErr != nil {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return strings.EqualFold(leftAbs, rightAbs)
}

func encodeControlOutput(output any) error {
	return json.NewEncoder(os.Stdout).Encode(output)
}

func writeGatewayResult(request protocol.Request, payload json.RawMessage, err error) {
	if err != nil {
		if gatewayErr, ok := err.(*gateway.Error); ok {
			response, responseErr := protocol.NewErrorResponse(request.RequestID, gatewayErr.Code, gatewayErr.Message, gatewayErr.Details)
			if responseErr != nil {
				fmt.Fprintln(os.Stderr, responseErr)
				os.Exit(1)
			}
			writeResponse(response)
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	response, responseErr := protocol.NewSuccessResponse(request.RequestID, payload)
	if responseErr != nil {
		fmt.Fprintln(os.Stderr, responseErr)
		os.Exit(1)
	}
	writeResponse(response)
}

func writeSuccess(requestID string, payload any) {
	response, err := protocol.NewSuccessResponse(requestID, payload)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	writeResponse(response)
}

func writeError(requestID, code, message string, details map[string]any) {
	response, err := protocol.NewErrorResponse(requestID, code, message, details)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	writeResponse(response)
}

func writeResponse(response protocol.Response) {
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
