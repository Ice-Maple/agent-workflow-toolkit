// Package artifact stages provider output into an explicitly assigned task
// workspace path and returns a hashable, validated reference.
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agent-workflow-toolkit/internal/orchestration"
)

func Stage(workspace, path, format, content string, requiredSections []string) (orchestration.ArtifactRef, error) {
	if strings.TrimSpace(content) == "" {
		return orchestration.ArtifactRef{}, errors.New("artifact content is empty")
	}
	canonicalWorkspace, err := orchestration.CanonicalDirectory(workspace)
	if err != nil {
		return orchestration.ArtifactRef{}, err
	}
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
		return orchestration.ArtifactRef{}, errors.New("artifact path must be absolute")
	}
	artifactPath, err := filepath.Abs(path)
	if err != nil {
		return orchestration.ArtifactRef{}, err
	}
	artifactPath = filepath.Clean(artifactPath)
	if err := ensureInside(canonicalWorkspace, artifactPath); err != nil {
		return orchestration.ArtifactRef{}, err
	}
	if err := recoverPrevious(artifactPath); err != nil {
		return orchestration.ArtifactRef{}, err
	}
	if _, err := os.Stat(artifactPath); err == nil {
		resolved, resolveErr := filepath.EvalSymlinks(artifactPath)
		if resolveErr != nil {
			return orchestration.ArtifactRef{}, fmt.Errorf("resolve artifact path: %w", resolveErr)
		}
		if err := ensureInside(canonicalWorkspace, filepath.Clean(resolved)); err != nil {
			return orchestration.ArtifactRef{}, errors.New("artifact path resolves outside task workspace")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return orchestration.ArtifactRef{}, fmt.Errorf("inspect artifact path: %w", err)
	}
	parent := filepath.Dir(artifactPath)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return orchestration.ArtifactRef{}, fmt.Errorf("create artifact directory: %w", err)
	}
	canonicalParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return orchestration.ArtifactRef{}, fmt.Errorf("resolve artifact directory: %w", err)
	}
	if err := ensureInside(canonicalWorkspace, filepath.Clean(canonicalParent)); err != nil {
		return orchestration.ArtifactRef{}, errors.New("artifact directory resolves outside task workspace")
	}
	if format == "markdown" {
		extension := strings.ToLower(filepath.Ext(artifactPath))
		if extension != ".md" && extension != ".markdown" {
			return orchestration.ArtifactRef{}, errors.New("markdown artifact must use .md or .markdown extension")
		}
	}
	for _, section := range requiredSections {
		if strings.TrimSpace(section) != "" && !strings.Contains(content, section) {
			return orchestration.ArtifactRef{}, fmt.Errorf("required artifact section %q is missing", section)
		}
	}
	raw := []byte(content)
	if err := atomicReplace(artifactPath, raw); err != nil {
		return orchestration.ArtifactRef{}, err
	}
	sum := sha256.Sum256(raw)
	return orchestration.ArtifactRef{Path: artifactPath, Format: format, Bytes: int64(len(raw)), SHA256: hex.EncodeToString(sum[:]), Validated: true}, nil
}

func ensureInside(workspace, path string) error {
	relative, err := filepath.Rel(workspace, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("artifact path is outside task workspace")
	}
	return nil
}

func atomicReplace(path string, raw []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".artifact-*")
	if err != nil {
		return fmt.Errorf("create artifact temporary file: %w", err)
	}
	temporary := file.Name()
	defer func() {
		_ = os.Remove(temporary)
	}()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("set artifact temporary permissions: %w", err)
	}
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return fmt.Errorf("write artifact temporary file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync artifact temporary file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close artifact temporary file: %w", err)
	}
	if err := os.Rename(temporary, path); err == nil {
		_ = os.Remove(path + ".previous")
		return nil
	}
	// Windows does not replace an existing file with Rename. Move the old
	// artifact to a recoverable sibling, install the new file, and restore the
	// old path if installation fails.
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("replace artifact: %w", err)
	}
	backup := path + ".previous"
	_ = os.Remove(backup)
	if err := os.Rename(path, backup); err != nil {
		return fmt.Errorf("backup existing artifact: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Rename(backup, path)
		return fmt.Errorf("install artifact: %w", err)
	}
	_ = os.Remove(backup)
	return nil
}

// recoverPrevious restores the last complete artifact when a process was
// interrupted after moving the destination aside but before installing the
// replacement. A later Stage call can then perform a normal atomic replace.
func recoverPrevious(path string) error {
	backup := path + ".previous"
	if _, err := os.Stat(backup); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect previous artifact: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		_ = os.Remove(backup)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect artifact during recovery: %w", err)
	}
	if err := os.Rename(backup, path); err != nil {
		return fmt.Errorf("restore previous artifact: %w", err)
	}
	return nil
}
