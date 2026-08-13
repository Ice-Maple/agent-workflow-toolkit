package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent-workflow-toolkit/internal/buildinfo"
	"agent-workflow-toolkit/internal/collaboration"
	"agent-workflow-toolkit/internal/coordination"
	"agent-workflow-toolkit/internal/gateway"
	"agent-workflow-toolkit/internal/orchestration"
	"agent-workflow-toolkit/internal/policy"
	"agent-workflow-toolkit/internal/roles"
	"agent-workflow-toolkit/internal/storage"
)

type routeOutput struct {
	SchemaVersion      string                                `json:"schema_version"`
	EffectiveMode      string                                `json:"effective_mode"`
	Degraded           bool                                  `json:"degraded"`
	Reason             string                                `json:"reason,omitempty"`
	RequestedInstances int                                   `json:"requested_instances"`
	ActualInstances    int                                   `json:"actual_instances"`
	Plan               orchestration.CollaborationPlan       `json:"plan"`
	Task               *gateway.CreateTaskResult             `json:"task,omitempty"`
	Run                *gateway.CreateCollaborationRunResult `json:"run,omitempty"`
}

func main() {
	showVersion := flag.Bool("version", false, "print the collaboration router version")
	manifestPath := flag.String("collaboration-file", "", "business Skill collaboration.yaml")
	rolesManifest := flag.String("roles-manifest", "", "Workflow/project roles.yaml")
	workspace := flag.String("workspace", "", "canonical business workspace")
	objective := flag.String("objective", "", "business objective preserved in the Task revision")
	title := flag.String("title", "", "optional Task title")
	stateDir := flag.String("state-dir", "", "new task state directory; required in framework mode")
	coordinationDir := flag.String("coordination-dir", "", "machine-local collaboration coordination directory")
	policyFile := flag.String("policy-file", "", "machine-local role routes and Adapter profiles")
	lockTimeout := flag.Duration("lock-timeout", 5*time.Second, "maximum coordination lock wait")
	flag.Parse()
	if *showVersion {
		fmt.Println(buildinfo.Format("agent-workflow-collaboration", 1))
		return
	}

	manifest, err := collaboration.Load(*manifestPath)
	if err != nil {
		fatal(err)
	}
	registry, err := roles.LoadManifest(*rolesManifest)
	if err != nil {
		fatal(err)
	}
	roleHashes := make(map[string]string, len(manifest.RoleDependencies))
	for _, role := range manifest.RoleDependencies {
		definition, ok := registry.Definitions[role]
		if !ok {
			fatal(fmt.Errorf("role dependency %q is not present in the selected Role Package", role))
		}
		roleHashes[role] = definition.PromptHash
	}
	for _, step := range manifest.Steps {
		if _, ok := roleHashes[step.Role]; ok {
			continue
		}
		definition, ok := registry.Definitions[step.Role]
		if !ok {
			fatal(fmt.Errorf("step role %q is not present in the selected Role Package", step.Role))
		}
		roleHashes[step.Role] = definition.PromptHash
	}
	plan := manifest.Plan(roleHashes)
	requested := requestedInstances(plan)
	coordRoot, err := resolveCoordinationRoot(*coordinationDir)
	if err != nil {
		fatal(err)
	}
	manager, err := coordination.New(coordRoot, *lockTimeout)
	if err != nil {
		fatal(err)
	}
	status, err := manager.Status(*workspace)
	if err != nil {
		fatal(err)
	}
	output := routeOutput{SchemaVersion: collaboration.SchemaVersion, EffectiveMode: status.EffectiveMode, Degraded: status.Degraded, Reason: status.Reason, RequestedInstances: requested, Plan: plan}
	if status.EffectiveMode != coordination.ModeFramework {
		if status.EffectiveMode == coordination.ModeSingle {
			output.ActualInstances = 1
			if requested > 1 {
				output.Degraded = true
				output.Reason = "single mode uses the current Agent only; independent instance count is not preserved"
			}
		} else {
			output.ActualInstances = 0
			if requested > 1 && output.Reason == "" {
				output.Degraded = true
				output.Reason = "direct mode delegates execution to the host and does not guarantee independent instance count"
			}
		}
		writeJSON(output)
		return
	}
	if strings.TrimSpace(*stateDir) == "" || strings.TrimSpace(*objective) == "" {
		fatal(errors.New("framework mode requires --state-dir and --objective"))
	}
	config, err := loadPolicy(*policyFile)
	if err != nil {
		fatal(err)
	}
	config.RoleDefinitions = registry.Definitions
	if err := config.Validate(); err != nil {
		fatal(err)
	}
	locks, err := manager.AcquireRunLocks(*stateDir)
	if err != nil {
		fatal(err)
	}
	defer locks.Release()
	store, err := storage.New(*stateDir)
	if err != nil {
		fatal(err)
	}
	core, err := gateway.NewWithCoordinationLocked(store, config, manager)
	if err != nil {
		fatal(err)
	}
	task, err := core.CreateTask(gateway.CreateTaskInput{Title: *title, Objective: *objective, Workspace: *workspace})
	if err != nil {
		fatal(err)
	}
	run, err := core.CreateCollaborationRun(gateway.CreateCollaborationRunInput{TaskID: task.Task.ID, RevisionID: task.Revision.ID, Workspace: task.Task.Workspace, EffectiveMode: orchestration.CollaborationFramework, GateEpoch: status.GateEpoch, Plan: plan, RolePromptHashes: roleHashes, StateDir: store.Root()})
	if err != nil {
		fatal(err)
	}
	output.ActualInstances = len(run.Instances)
	output.Task, output.Run = &task, &run
	writeJSON(output)
}

func requestedInstances(plan orchestration.CollaborationPlan) int {
	total := 0
	for _, step := range plan.Steps {
		count := step.Count
		if count == 0 {
			count = 1
		}
		total += count
	}
	return total
}

func loadPolicy(path string) (policy.Config, error) {
	if strings.TrimSpace(path) == "" {
		return policy.DefaultConfig(), nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return policy.Config{}, err
	}
	return policy.LoadJSON(raw)
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
