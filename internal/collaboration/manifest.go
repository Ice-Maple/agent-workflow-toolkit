// Package collaboration loads the intentionally small role-first sidecar
// owned by a business Skill. It does not interpret business semantics or
// Provider configuration.
package collaboration

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"agent-workflow-toolkit/internal/orchestration"
	"gopkg.in/yaml.v3"
)

const SchemaVersion = "collaboration/v1"

type Manifest struct {
	SchemaVersion    string                                `json:"schema_version" yaml:"schema_version"`
	RoleDependencies []string                              `json:"role_dependencies,omitempty" yaml:"role_dependencies,omitempty"`
	Steps            []orchestration.CollaborationStepSpec `json:"steps" yaml:"steps"`
}

func Load(path string) (Manifest, error) {
	if strings.TrimSpace(path) == "" {
		return Manifest{}, errors.New("collaboration manifest path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read collaboration manifest: %w", err)
	}
	var manifest Manifest
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode collaboration manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Manifest{}, errors.New("collaboration manifest contains more than one YAML document")
		}
		return Manifest{}, fmt.Errorf("decode collaboration manifest trailing data: %w", err)
	}
	if manifest.SchemaVersion != SchemaVersion {
		return Manifest{}, fmt.Errorf("unsupported collaboration schema %q", manifest.SchemaVersion)
	}
	if len(manifest.Steps) == 0 {
		return Manifest{}, errors.New("collaboration manifest requires at least one step")
	}
	dependencySet := map[string]struct{}{}
	for _, role := range manifest.RoleDependencies {
		role = strings.TrimSpace(role)
		if role == "" {
			return Manifest{}, errors.New("role_dependencies cannot contain an empty role")
		}
		dependencySet[role] = struct{}{}
	}
	seen := map[string]struct{}{}
	for index := range manifest.Steps {
		step := &manifest.Steps[index]
		step.ID, step.Role, step.Task = strings.TrimSpace(step.ID), strings.TrimSpace(step.Role), strings.TrimSpace(step.Task)
		if step.ID == "" || step.Role == "" || step.Task == "" {
			return Manifest{}, fmt.Errorf("step %d requires id, role and task", index)
		}
		if step.Count < 0 {
			return Manifest{}, fmt.Errorf("step %q count cannot be negative", step.ID)
		}
		if _, ok := seen[step.ID]; ok {
			return Manifest{}, fmt.Errorf("duplicate collaboration step %q", step.ID)
		}
		seen[step.ID] = struct{}{}
		if len(dependencySet) > 0 {
			if _, ok := dependencySet[step.Role]; !ok {
				return Manifest{}, fmt.Errorf("step %q role %q is not declared in role_dependencies", step.ID, step.Role)
			}
		}
	}
	for _, step := range manifest.Steps {
		for _, dependency := range step.After {
			if _, ok := seen[dependency]; !ok {
				return Manifest{}, fmt.Errorf("step %q depends on unknown step %q", step.ID, dependency)
			}
		}
	}
	return manifest, nil
}

func (m Manifest) Plan(rolePromptHashes map[string]string) orchestration.CollaborationPlan {
	steps := append([]orchestration.CollaborationStepSpec(nil), m.Steps...)
	return orchestration.CollaborationPlan{SchemaVersion: SchemaVersion, RolePromptHashes: rolePromptHashes, Steps: steps}
}
