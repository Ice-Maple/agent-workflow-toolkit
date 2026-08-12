// Package roles loads and verifies the complete Role Prompts owned by a
// Workflow or project. The Gateway only persists the resulting identity and
// hash; prompt text remains in the package and is returned to a caller only
// after the hash has been checked.
package roles

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"agent-workflow-toolkit/internal/orchestration"
	"gopkg.in/yaml.v3"
)

const SchemaVersion = "collaboration-roles/v1"

// Manifest is the intentionally small roles.yaml contract. Role behavior is
// kept in the referenced Markdown file rather than split into a second DSL.
type Manifest struct {
	SchemaVersion string           `yaml:"schema_version"`
	Package       string           `yaml:"package"`
	Version       string           `yaml:"version"`
	Roles         map[string]Entry `yaml:"roles"`
}

type Entry struct {
	Prompt     string `yaml:"prompt"`
	SHA256     string `yaml:"sha256,omitempty"`
	PromptHash string `yaml:"prompt_hash,omitempty"`
}

// Registry is safe to copy after loading. Definitions are public so an
// installer or receipt writer can persist only non-secret source metadata;
// prompts are deliberately kept in an unexported map.
type Registry struct {
	Package     string
	Version     string
	Definitions map[string]orchestration.RoleDefinition
	prompts     map[string]string
}

// LoadPackage loads <directory>/roles.yaml and every referenced prompt.
func LoadPackage(directory string) (Registry, error) {
	if strings.TrimSpace(directory) == "" {
		return Registry{}, errors.New("role package directory is required")
	}
	manifestPath := filepath.Join(directory, "roles.yaml")
	return LoadManifest(manifestPath)
}

// LoadManifest loads a roles.yaml file and resolves all prompt paths relative
// to the manifest directory. Symlinks that resolve outside the package are
// rejected so a package cannot silently borrow an unrelated local prompt.
func LoadManifest(manifestPath string) (Registry, error) {
	if strings.TrimSpace(manifestPath) == "" {
		return Registry{}, errors.New("roles manifest path is required")
	}
	manifestPath, err := filepath.Abs(filepath.Clean(manifestPath))
	if err != nil {
		return Registry{}, fmt.Errorf("resolve roles manifest: %w", err)
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return Registry{}, fmt.Errorf("read roles manifest: %w", err)
	}
	var manifest Manifest
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return Registry{}, fmt.Errorf("decode roles manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Registry{}, errors.New("roles manifest contains more than one YAML document")
		}
		return Registry{}, fmt.Errorf("decode roles manifest trailing data: %w", err)
	}
	return loadManifest(filepath.Dir(manifestPath), manifest)
}

func loadManifest(packageDir string, manifest Manifest) (Registry, error) {
	if manifest.SchemaVersion != SchemaVersion {
		return Registry{}, fmt.Errorf("unsupported role schema %q; expected %q", manifest.SchemaVersion, SchemaVersion)
	}
	manifest.Package = strings.TrimSpace(manifest.Package)
	manifest.Version = strings.TrimSpace(manifest.Version)
	if manifest.Package == "" || manifest.Version == "" {
		return Registry{}, errors.New("role manifest package and version are required")
	}
	if len(manifest.Roles) == 0 {
		return Registry{}, errors.New("role manifest must declare at least one role")
	}
	packageDir, err := filepath.Abs(filepath.Clean(packageDir))
	if err != nil {
		return Registry{}, fmt.Errorf("resolve role package: %w", err)
	}
	packageInfo, err := os.Stat(packageDir)
	if err != nil || !packageInfo.IsDir() {
		if err == nil {
			err = errors.New("path is not a directory")
		}
		return Registry{}, fmt.Errorf("role package is not accessible: %w", err)
	}
	packageRoot, err := filepath.EvalSymlinks(packageDir)
	if err != nil {
		return Registry{}, fmt.Errorf("canonicalize role package: %w", err)
	}
	registry := Registry{Package: manifest.Package, Version: manifest.Version, Definitions: map[string]orchestration.RoleDefinition{}, prompts: map[string]string{}}
	roleNames := make([]string, 0, len(manifest.Roles))
	for name := range manifest.Roles {
		roleNames = append(roleNames, name)
	}
	sort.Strings(roleNames)
	for _, name := range roleNames {
		entry := manifest.Roles[name]
		name = strings.TrimSpace(name)
		if err := validateRoleName(name); err != nil {
			return Registry{}, fmt.Errorf("role %q: %w", name, err)
		}
		if strings.TrimSpace(entry.Prompt) == "" {
			return Registry{}, fmt.Errorf("role %q prompt is required", name)
		}
		promptPath, err := safePromptPath(packageRoot, entry.Prompt)
		if err != nil {
			return Registry{}, fmt.Errorf("role %q prompt: %w", name, err)
		}
		promptInfo, err := os.Stat(promptPath)
		if err != nil {
			return Registry{}, fmt.Errorf("role %q prompt is not accessible: %w", name, err)
		}
		if !promptInfo.Mode().IsRegular() {
			return Registry{}, fmt.Errorf("role %q prompt must be a regular file", name)
		}
		prompt, err := os.ReadFile(promptPath)
		if err != nil {
			return Registry{}, fmt.Errorf("read role %q prompt: %w", name, err)
		}
		hash := hashPrompt(prompt)
		declaredHash := strings.TrimSpace(entry.SHA256)
		if alternate := strings.TrimSpace(entry.PromptHash); alternate != "" {
			if declaredHash != "" && !strings.EqualFold(declaredHash, alternate) {
				return Registry{}, fmt.Errorf("role %q declares conflicting prompt hashes", name)
			}
			declaredHash = alternate
		}
		if declaredHash != "" && !strings.EqualFold(declaredHash, hash) {
			return Registry{}, fmt.Errorf("role %q prompt hash mismatch: manifest=%s actual=%s", name, declaredHash, hash)
		}
		roleID := manifest.Package + "/" + name
		registry.Definitions[roleID] = orchestration.RoleDefinition{ID: roleID, Package: manifest.Package, Version: manifest.Version, PromptPath: promptPath, PromptHash: hash}
		registry.prompts[roleID] = string(prompt)
	}
	return registry, nil
}

// Merge combines Workflow/project registries and rejects same-ID conflicts.
// Identical package/version/hash records are idempotent and retained once.
func Merge(registries ...Registry) (Registry, error) {
	merged := Registry{Definitions: map[string]orchestration.RoleDefinition{}, prompts: map[string]string{}}
	for _, registry := range registries {
		if registry.Package != "" {
			if merged.Package == "" {
				merged.Package = registry.Package
				merged.Version = registry.Version
			}
		}
		for id, definition := range registry.Definitions {
			if existing, ok := merged.Definitions[id]; ok {
				if existing.Package != definition.Package || existing.Version != definition.Version || !strings.EqualFold(existing.PromptHash, definition.PromptHash) {
					return Registry{}, fmt.Errorf("role %q has conflicting definitions", id)
				}
				continue
			}
			merged.Definitions[id] = definition
			merged.prompts[id] = registry.prompts[id]
		}
	}
	if len(merged.Definitions) == 0 {
		return Registry{}, errors.New("role registry is empty")
	}
	return merged, nil
}

func (r Registry) Resolve(roleID string) (orchestration.RoleDefinition, error) {
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return orchestration.RoleDefinition{}, errors.New("role id is required")
	}
	definition, ok := r.Definitions[roleID]
	if !ok {
		return orchestration.RoleDefinition{}, fmt.Errorf("role %q is not registered", roleID)
	}
	return definition, nil
}

// Prompt returns the complete prompt only when the caller supplies the hash
// frozen by the registry. An empty expected hash is rejected deliberately.
func (r Registry) Prompt(roleID, expectedHash string) (string, orchestration.RoleDefinition, error) {
	definition, err := r.Resolve(roleID)
	if err != nil {
		return "", orchestration.RoleDefinition{}, err
	}
	if strings.TrimSpace(expectedHash) == "" || !strings.EqualFold(strings.TrimSpace(expectedHash), definition.PromptHash) {
		return "", orchestration.RoleDefinition{}, fmt.Errorf("role %q prompt hash does not match the registered content", roleID)
	}
	prompt, ok := r.prompts[roleID]
	if !ok {
		return "", orchestration.RoleDefinition{}, fmt.Errorf("role %q prompt content is unavailable", roleID)
	}
	return prompt, definition, nil
}

func (r Registry) Hashes() map[string]string {
	hashes := make(map[string]string, len(r.Definitions))
	for id, definition := range r.Definitions {
		hashes[id] = definition.PromptHash
	}
	return hashes
}

func validateRoleName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) || strings.ContainsAny(name, "\r\n\t") {
		return errors.New("role name must be a single non-empty path segment")
	}
	return nil
}

func safePromptPath(packageRoot, prompt string) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || filepath.IsAbs(prompt) {
		return "", errors.New("prompt path must be a non-empty relative path")
	}
	candidate := filepath.Join(packageRoot, filepath.Clean(prompt))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve prompt path: %w", err)
	}
	if !isWithin(packageRoot, resolved) {
		return "", errors.New("prompt path resolves outside the role package")
	}
	return filepath.Clean(resolved), nil
}

func isWithin(root, child string) bool {
	relative, err := filepath.Rel(root, child)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func hashPrompt(prompt []byte) string {
	sum := sha256.Sum256(prompt)
	return hex.EncodeToString(sum[:])
}
