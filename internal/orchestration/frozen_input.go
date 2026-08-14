package orchestration

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const MaxFrozenInputBytes int64 = 16 * 1024 * 1024

// VerifyFrozenInputs canonicalizes and re-hashes task-scoped read-only input
// artifacts. Every root must contain only the declared regular files so an
// Adapter can grant that root without exposing unrelated machine data.
func VerifyFrozenInputs(inputs []FrozenInputRef) ([]FrozenInputRef, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	normalized := make([]FrozenInputRef, len(inputs))
	expectedByRoot := map[string]map[string]struct{}{}
	seenNames := map[string]struct{}{}
	for index, input := range inputs {
		input.Name = strings.TrimSpace(input.Name)
		input.Path = filepath.Clean(strings.TrimSpace(input.Path))
		input.Root = filepath.Clean(strings.TrimSpace(input.Root))
		input.Format = strings.TrimSpace(input.Format)
		input.SHA256 = strings.ToLower(strings.TrimSpace(input.SHA256))
		if input.Name == "" || !filepath.IsAbs(input.Path) || !filepath.IsAbs(input.Root) || len(input.SHA256) != 64 || input.Bytes <= 0 || input.Bytes > MaxFrozenInputBytes {
			return nil, fmt.Errorf("input %d has invalid name, absolute path/root, bytes or SHA-256 metadata", index)
		}
		if _, err := hex.DecodeString(input.SHA256); err != nil {
			return nil, fmt.Errorf("input %q SHA-256 is not hexadecimal", input.Name)
		}
		if _, duplicate := seenNames[input.Name]; duplicate {
			return nil, fmt.Errorf("duplicate frozen input name %q", input.Name)
		}
		seenNames[input.Name] = struct{}{}
		canonicalRoot, err := filepath.EvalSymlinks(input.Root)
		if err != nil {
			return nil, fmt.Errorf("canonicalize input %q root: %w", input.Name, err)
		}
		canonicalPath, err := filepath.EvalSymlinks(input.Path)
		if err != nil {
			return nil, fmt.Errorf("canonicalize input %q path: %w", input.Name, err)
		}
		if !sameFrozenPath(filepath.Dir(canonicalPath), canonicalRoot) {
			return nil, fmt.Errorf("input %q must be a direct child of its dedicated root", input.Name)
		}
		info, err := os.Stat(canonicalPath)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("input %q is not an accessible regular file", input.Name)
		}
		if info.Size() != input.Bytes {
			return nil, fmt.Errorf("input %q byte length changed: expected=%d actual=%d", input.Name, input.Bytes, info.Size())
		}
		raw, err := os.ReadFile(canonicalPath)
		if err != nil {
			return nil, fmt.Errorf("read input %q: %w", input.Name, err)
		}
		sum := sha256.Sum256(raw)
		actualHash := hex.EncodeToString(sum[:])
		if !strings.EqualFold(actualHash, input.SHA256) {
			return nil, fmt.Errorf("input %q SHA-256 changed: expected=%s actual=%s", input.Name, input.SHA256, actualHash)
		}
		input.Path, input.Root = canonicalPath, canonicalRoot
		normalized[index] = input
		if expectedByRoot[canonicalRoot] == nil {
			expectedByRoot[canonicalRoot] = map[string]struct{}{}
		}
		expectedByRoot[canonicalRoot][strings.ToLower(filepath.Base(canonicalPath))] = struct{}{}
	}
	for root, expected := range expectedByRoot {
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, fmt.Errorf("read frozen input root: %w", err)
		}
		if len(entries) != len(expected) {
			return nil, errors.New("frozen input root contains undeclared entries")
		}
		for _, entry := range entries {
			if entry.IsDir() {
				return nil, errors.New("frozen input root contains a directory")
			}
			if _, ok := expected[strings.ToLower(entry.Name())]; !ok {
				return nil, errors.New("frozen input root contains an undeclared file")
			}
		}
	}
	return normalized, nil
}

func sameFrozenPath(left, right string) bool {
	return sameFrozenPathForOS(runtime.GOOS, left, right)
}

func sameFrozenPathForOS(goos, left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	if goos == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
