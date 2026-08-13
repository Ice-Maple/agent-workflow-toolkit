package collaboration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMinimalRoleFirstManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collaboration.yaml")
	raw := []byte("schema_version: collaboration/v1\nrole_dependencies: [workflow/reviewer]\nsteps:\n  - id: draft\n    role: workflow/reviewer\n    task: review the current change\n  - id: verify\n    role: workflow/reviewer\n    task: verify the findings\n    count: 2\n    after: [draft]\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Steps) != 2 || manifest.Steps[1].Count != 2 || manifest.Steps[1].After[0] != "draft" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if plan := manifest.Plan(map[string]string{"workflow/reviewer": "hash"}); plan.SchemaVersion != SchemaVersion || plan.RolePromptHashes["workflow/reviewer"] != "hash" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestLoadRejectsProviderFieldsAndMissingRoleDependency(t *testing.T) {
	for name, raw := range map[string]string{
		"provider":   "schema_version: collaboration/v1\nsteps:\n  - id: review\n    role: workflow/reviewer\n    task: review\n    adapter: cursor\n",
		"dependency": "schema_version: collaboration/v1\nrole_dependencies: [workflow/writer]\nsteps:\n  - id: review\n    role: workflow/reviewer\n    task: review\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "collaboration.yaml")
			if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("Load() accepted an invalid business sidecar")
			}
		})
	}
}
