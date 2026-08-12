package roles

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadManifestResolvesAndHashesPrompt(t *testing.T) {
	root := t.TempDir()
	prompt := []byte("# Reviewer\n\nReview the actual files.\n")
	if err := os.WriteFile(filepath.Join(root, "reviewer.md"), prompt, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := "schema_version: collaboration-roles/v1\npackage: sample-workflow\nversion: 1.2.3\nroles:\n  reviewer:\n    prompt: reviewer.md\n"
	manifestPath := filepath.Join(root, "roles.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := registry.Resolve("sample-workflow/reviewer")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(prompt)
	if definition.PromptHash != hex.EncodeToString(sum[:]) {
		t.Fatalf("prompt hash = %q, want %q", definition.PromptHash, hex.EncodeToString(sum[:]))
	}
	loaded, resolved, err := registry.Prompt(definition.ID, definition.PromptHash)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != string(prompt) || resolved != definition {
		t.Fatalf("resolved prompt = %q, definition = %#v", loaded, resolved)
	}
	if _, _, err := registry.Prompt(definition.ID, "wrong"); err == nil {
		t.Fatal("prompt was returned for a mismatched hash")
	}
}

func TestLoadManifestRejectsUnknownFieldsAndHashMismatch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "reviewer.md"), []byte("review"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"unknown field": "schema_version: collaboration-roles/v1\npackage: sample\nversion: 1\nextra: true\nroles:\n  reviewer:\n    prompt: reviewer.md\n",
		"hash mismatch": "schema_version: collaboration-roles/v1\npackage: sample\nversion: 1\nroles:\n  reviewer:\n    prompt: reviewer.md\n    sha256: 0000000000000000000000000000000000000000000000000000000000000000\n",
	}
	for name, manifest := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, strings.ReplaceAll(name, " ", "-")+".yaml")
			if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadManifest(path); err == nil {
				t.Fatal("invalid role manifest was accepted")
			}
		})
	}
}

func TestMergeRejectsConflictingRoleID(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{"a.md": "one", "b.md": "two"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	firstDir := filepath.Join(root, "first")
	secondDir := filepath.Join(root, "second")
	if err := os.MkdirAll(firstDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secondDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(firstDir, "a.md"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secondDir, "b.md"), []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := func(dir, prompt string) string {
		return "schema_version: collaboration-roles/v1\npackage: sample\nversion: 1\nroles:\n  reviewer:\n    prompt: " + prompt + "\n"
	}
	if err := os.WriteFile(filepath.Join(firstDir, "roles.yaml"), []byte(manifest(firstDir, "a.md")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secondDir, "roles.yaml"), []byte(manifest(secondDir, "b.md")), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := LoadPackage(firstDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadPackage(secondDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Merge(first, second); err == nil {
		t.Fatal("conflicting role definitions were merged")
	}
}

func TestLoadManifestRejectsPromptOutsidePackage(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := "schema_version: collaboration-roles/v1\npackage: sample\nversion: 1\nroles:\n  reviewer:\n    prompt: " + outside + "\n"
	path := filepath.Join(root, "roles.yaml")
	if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(path); err == nil {
		t.Fatal("outside prompt was accepted")
	}
}
