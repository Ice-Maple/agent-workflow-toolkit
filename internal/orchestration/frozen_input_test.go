package orchestration

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyFrozenInputsEnforcesDedicatedImmutableRoot(t *testing.T) {
	t.Run("valid multiple declared files", func(t *testing.T) {
		root := t.TempDir()
		inputs := []FrozenInputRef{
			writeFrozenInputForTest(t, root, "status.md", "status", "# status\n"),
			writeFrozenInputForTest(t, root, "diff.md", "diff", "# diff\n"),
		}
		verified, err := VerifyFrozenInputs(inputs)
		if err != nil || len(verified) != 2 {
			t.Fatalf("VerifyFrozenInputs() verified=%#v error=%v", verified, err)
		}
	})

	t.Run("undeclared file", func(t *testing.T) {
		root := t.TempDir()
		input := writeFrozenInputForTest(t, root, "review.md", "review", "# review\n")
		if err := os.WriteFile(filepath.Join(root, "extra.txt"), []byte("extra"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := VerifyFrozenInputs([]FrozenInputRef{input}); err == nil || !strings.Contains(err.Error(), "undeclared") {
			t.Fatalf("undeclared file error = %v", err)
		}
	})

	t.Run("path is not direct child", func(t *testing.T) {
		root := t.TempDir()
		nested := filepath.Join(root, "nested")
		if err := os.Mkdir(nested, 0o700); err != nil {
			t.Fatal(err)
		}
		input := writeFrozenInputForTest(t, nested, "review.md", "review", "# review\n")
		input.Root = root
		if _, err := VerifyFrozenInputs([]FrozenInputRef{input}); err == nil || !strings.Contains(err.Error(), "direct child") {
			t.Fatalf("nested path error = %v", err)
		}
	})

	t.Run("SHA-256 drift", func(t *testing.T) {
		root := t.TempDir()
		input := writeFrozenInputForTest(t, root, "review.md", "review", "# review\n")
		input.SHA256 = strings.Repeat("0", 64)
		if _, err := VerifyFrozenInputs([]FrozenInputRef{input}); err == nil || !strings.Contains(err.Error(), "SHA-256 changed") {
			t.Fatalf("SHA drift error = %v", err)
		}
	})
}

func TestSameFrozenPathUsesOperatingSystemCaseSemantics(t *testing.T) {
	left := filepath.Join("root", "Review")
	right := filepath.Join("root", "review")
	for _, test := range []struct {
		goos string
		want bool
	}{{goos: "windows", want: true}, {goos: "linux", want: false}, {goos: "darwin", want: false}} {
		if got := sameFrozenPathForOS(test.goos, left, right); got != test.want {
			t.Fatalf("sameFrozenPathForOS(%q, %q, %q) = %t, want %t", test.goos, left, right, got, test.want)
		}
	}
}

func writeFrozenInputForTest(t *testing.T, root, name, logicalName, content string) FrozenInputRef {
	t.Helper()
	path := filepath.Join(root, name)
	raw := []byte(content)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return FrozenInputRef{Name: logicalName, Path: path, Root: root, Format: "markdown", Bytes: int64(len(raw)), SHA256: fmt.Sprintf("%x", sum[:])}
}
