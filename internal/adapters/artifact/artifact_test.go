package artifact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageWritesValidatedWorkspaceArtifact(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "evidence", "review.md")
	ref, err := Stage(workspace, path, "markdown", "# Review\n\nPASS\n", []string{"# Review", "PASS"})
	if err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	if !ref.Validated || ref.Bytes == 0 || ref.SHA256 == "" || ref.Path != path {
		t.Fatalf("artifact ref = %#v", ref)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "# Review\n\nPASS\n" {
		t.Fatalf("artifact content = %q, err=%v", raw, err)
	}
}

func TestStageRejectsOutsideAndInvalidContent(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(filepath.Dir(workspace), "outside.md")
	if _, err := Stage(workspace, outside, "markdown", "# Review", nil); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("outside artifact error = %v", err)
	}
	if _, err := Stage(workspace, filepath.Join(workspace, "review.md"), "markdown", "", nil); err == nil {
		t.Fatal("empty artifact was accepted")
	}
	if _, err := Stage(workspace, filepath.Join(workspace, "review.txt"), "markdown", "# Review", nil); err == nil {
		t.Fatal("markdown artifact with wrong extension was accepted")
	}
}

func TestStageReplacesExistingArtifactAtomically(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "review.md")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(workspace, path, "markdown", "new", nil); err != nil {
		t.Fatalf("replace error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "new" {
		t.Fatalf("replaced artifact = %q, err=%v", raw, err)
	}
}
