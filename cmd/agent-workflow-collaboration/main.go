package main

import (
	"crypto/sha256"
	"encoding/hex"
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
	projectRole := flag.String("role", "", "registered project Role ID for a one-step managed dispatch")
	projectTask := flag.String("task", "", "task payload for --role")
	frozenInputPath := flag.String("frozen-input", "", "host-created read-only input artifact for --role")
	frozenInputHash := flag.String("frozen-input-sha256", "", "expected SHA-256 for --frozen-input")
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

	manifest, err := loadCollaborationInput(*manifestPath, *projectRole, *projectTask, *frozenInputPath, *frozenInputHash)
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

func loadCollaborationInput(manifestPath, projectRole, projectTask, frozenInputPath, frozenInputHash string) (collaboration.Manifest, error) {
	hasManifest := strings.TrimSpace(manifestPath) != ""
	hasProjectRole := strings.TrimSpace(projectRole) != "" || strings.TrimSpace(projectTask) != ""
	if hasManifest == hasProjectRole {
		return collaboration.Manifest{}, errors.New("exactly one of --collaboration-file or --role/--task is required")
	}
	if hasManifest {
		if strings.TrimSpace(frozenInputPath) != "" || strings.TrimSpace(frozenInputHash) != "" {
			return collaboration.Manifest{}, errors.New("frozen project Role input cannot be combined with --collaboration-file")
		}
		return collaboration.Load(manifestPath)
	}
	projectRole, projectTask = strings.TrimSpace(projectRole), strings.TrimSpace(projectTask)
	if projectRole == "" || projectTask == "" {
		return collaboration.Manifest{}, errors.New("project Role dispatch requires both --role and --task")
	}
	var frozenInputs []orchestration.FrozenInputRef
	if strings.TrimSpace(frozenInputPath) != "" || strings.TrimSpace(frozenInputHash) != "" {
		input, err := loadFrozenInput(frozenInputPath, frozenInputHash)
		if err != nil {
			return collaboration.Manifest{}, err
		}
		frozenInputs = []orchestration.FrozenInputRef{input}
	}
	return collaboration.Manifest{
		SchemaVersion:    collaboration.SchemaVersion,
		RoleDependencies: []string{projectRole},
		Steps: []orchestration.CollaborationStepSpec{{
			ID: "project-role", Role: projectRole, Task: projectTask, Count: 1, FrozenInputs: frozenInputs,
		}},
	}, nil
}

func loadFrozenInput(path, expectedHash string) (orchestration.FrozenInputRef, error) {
	path, expectedHash = strings.TrimSpace(path), strings.ToLower(strings.TrimSpace(expectedHash))
	if path == "" || expectedHash == "" {
		return orchestration.FrozenInputRef{}, errors.New("--frozen-input and --frozen-input-sha256 are required together")
	}
	if len(expectedHash) != 64 {
		return orchestration.FrozenInputRef{}, errors.New("frozen input SHA-256 must contain 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(expectedHash); err != nil {
		return orchestration.FrozenInputRef{}, errors.New("frozen input SHA-256 is not hexadecimal")
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return orchestration.FrozenInputRef{}, fmt.Errorf("resolve frozen input: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return orchestration.FrozenInputRef{}, fmt.Errorf("canonicalize frozen input: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.Mode().IsRegular() {
		return orchestration.FrozenInputRef{}, errors.New("frozen input must be an accessible regular file")
	}
	if info.Size() <= 0 || info.Size() > 16*1024*1024 {
		return orchestration.FrozenInputRef{}, errors.New("frozen input must be between 1 byte and 16 MiB")
	}
	root := filepath.Dir(canonical)
	entries, err := os.ReadDir(root)
	if err != nil {
		return orchestration.FrozenInputRef{}, fmt.Errorf("read frozen input root: %w", err)
	}
	if len(entries) != 1 || entries[0].IsDir() || !strings.EqualFold(entries[0].Name(), filepath.Base(canonical)) {
		return orchestration.FrozenInputRef{}, errors.New("frozen input must be the only entry in its dedicated read-only root")
	}
	raw, err := os.ReadFile(canonical)
	if err != nil {
		return orchestration.FrozenInputRef{}, fmt.Errorf("read frozen input: %w", err)
	}
	sum := sha256.Sum256(raw)
	actualHash := hex.EncodeToString(sum[:])
	if !strings.EqualFold(expectedHash, actualHash) {
		return orchestration.FrozenInputRef{}, fmt.Errorf("frozen input SHA-256 mismatch: expected=%s actual=%s", expectedHash, actualHash)
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(canonical)), ".")
	if format == "" {
		format = "text"
	}
	return orchestration.FrozenInputRef{Name: "repository-review", Path: canonical, Root: root, Format: format, Bytes: info.Size(), SHA256: actualHash}, nil
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
