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
	"agent-workflow-toolkit/internal/gateway"
	"agent-workflow-toolkit/internal/orchestration"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--owner" {
		if code := bridge.RunOwnerMain(context.Background(), os.Args[1:]); code >= 0 {
			os.Exit(code)
		}
	}
	showVersion := flag.Bool("version", false, "print the external bridge version")
	hashProfileMode := flag.Bool("hash-profile", false, "print a profile with its canonical profile_hash without executing a Provider")
	preflightMode := flag.Bool("preflight", false, "run Adapter capability preflight from binding/profile files")
	executeMode := flag.Bool("execute", false, "execute one pending core HostAction through the external bridge")
	cancelMode := flag.Bool("cancel", false, "request and execute a core-issued cancel_host action")
	stateDir := flag.String("state-dir", "", "task state directory used by the core")
	bridgeStateDir := flag.String("bridge-state-dir", "", "machine-local bridge owner state directory")
	corePath := flag.String("core", "", "agent-workflow-core executable")
	policyFile := flag.String("policy-file", "", "machine-local policy JSON passed to the core")
	rolesManifest := flag.String("roles-manifest", "", "Workflow/project roles.yaml passed to the core")
	coordinationDir := flag.String("coordination-dir", "", "machine-local collaboration coordination directory")
	lockTimeout := flag.Duration("lock-timeout", 5*time.Second, "core state lock timeout")
	profileFile := flag.String("profile-file", "", "machine-local Adapter profile JSON")
	bindingFile := flag.String("binding-file", "", "binding JSON used by --preflight")
	attemptID := flag.String("attempt-id", "", "frozen Attempt ID")
	controllerID := flag.String("controller-instance-id", "", "Controller/bridge identity")
	actionID := flag.String("action-id", "", "exact core HostAction ID; required when more than one action is pending")
	actionKind := flag.String("action-kind", "", "optional HostAction filter: launch, observe or cancel_host")
	afterSequence := flag.Int("after-action-sequence", 0, "only consider actions after this core sequence")
	cancelReason := flag.String("reason", "bridge cancellation requested", "reason passed to requestCancel")
	flag.Parse()

	if *showVersion {
		fmt.Println(buildinfo.Format("agent-workflow-bridge", 1))
		return
	}
	modes := 0
	for _, selected := range []bool{*hashProfileMode, *preflightMode, *executeMode, *cancelMode} {
		if selected {
			modes++
		}
	}
	if modes != 1 {
		fatal(errors.New("exactly one of --hash-profile, --preflight, --execute or --cancel is required"))
	}
	if *hashProfileMode {
		profile, err := bridge.HashProfile(*profileFile)
		if err != nil {
			fatal(err)
		}
		writeJSON(profile)
		return
	}

	if *preflightMode {
		profile, err := bridge.LoadProfile(*profileFile)
		if err != nil {
			fatal(err)
		}
		binding, err := bridge.LoadBinding(*bindingFile)
		if err != nil {
			fatal(err)
		}
		result, err := bridge.AdapterPreflight(context.Background(), binding, profile)
		if err != nil {
			fatal(err)
		}
		writeJSON(result)
		return
	}

	if strings.TrimSpace(*stateDir) == "" || strings.TrimSpace(*profileFile) == "" || strings.TrimSpace(*attemptID) == "" || strings.TrimSpace(*controllerID) == "" {
		fatal(errors.New("--state-dir, --profile-file, --attempt-id and --controller-instance-id are required"))
	}
	profile, err := bridge.LoadProfile(*profileFile)
	if err != nil {
		fatal(err)
	}
	ownerRoot := *bridgeStateDir
	if strings.TrimSpace(ownerRoot) == "" {
		ownerRoot = filepath.Join(*stateDir, "bridge")
	}
	core := bridge.CoreClient{Executable: *corePath, StateDir: *stateDir, PolicyFile: *policyFile, RolesManifest: *rolesManifest, CoordinationDir: *coordinationDir, LockTimeout: *lockTimeout}
	selectedKind, err := parseActionKind(*actionKind)
	if err != nil {
		fatal(err)
	}
	selectedActionID := *actionID
	if *cancelMode {
		cancel, cancelErr := core.RequestCancel(context.Background(), gateway.CancelInput{AttemptID: *attemptID, Reason: *cancelReason})
		if cancelErr != nil {
			fatal(cancelErr)
		}
		selectedActionID = cancel.Action.ID
		selectedKind = orchestration.ActionCancelHost
	}
	output, err := (bridge.Executor{
		Core:                 core,
		BridgeStateDir:       ownerRoot,
		BridgeExecutable:     "",
		AttemptID:            *attemptID,
		ControllerInstanceID: *controllerID,
		ActionID:             selectedActionID,
		ActionKind:           selectedKind,
		AfterActionSequence:  *afterSequence,
		Profile:              profile,
	}).Execute(context.Background())
	if err != nil {
		fatal(err)
	}
	writeJSON(output)
}

func parseActionKind(raw string) (orchestration.ActionKind, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return "", nil
	case string(orchestration.ActionLaunch):
		return orchestration.ActionLaunch, nil
	case string(orchestration.ActionObserve):
		return orchestration.ActionObserve, nil
	case string(orchestration.ActionCancelHost):
		return orchestration.ActionCancelHost, nil
	default:
		return "", fmt.Errorf("unsupported action-kind %q", raw)
	}
}

func writeJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
