package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var ErrStateDirBusy = errors.New("state-dir is locked by another gateway process")

// RootLock serializes one-shot Gateway processes that share a state-dir.
// The operating system releases the lock when the process exits, including
// abnormal termination, so no stale lease file needs to be recovered.
type RootLock struct {
	file *os.File
	once sync.Once
	err  error
}

func AcquireRootLock(root string, timeout time.Duration) (*RootLock, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("storage root is required")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	root = filepath.Clean(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create storage root for lock: %w", err)
	}
	return AcquireFileLock(filepath.Join(root, ".gateway.lock"), timeout)
}

// AcquireFileLock obtains an exclusive OS lock for one named file. It is used
// by the collaboration coordination layer for the machine gate and active-run
// index, while keeping the same Windows and Unix locking implementation as the
// state-dir lock.
func AcquireFileLock(path string, timeout time.Duration) (*RootLock, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("lock path is required")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	path = filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	deadline := time.Now().Add(timeout)
	for {
		locked, lockErr := tryLockFile(file)
		if lockErr != nil {
			_ = file.Close()
			return nil, fmt.Errorf("lock state-dir: %w", lockErr)
		}
		if locked {
			return &RootLock{file: file}, nil
		}
		if !time.Now().Before(deadline) {
			_ = file.Close()
			return nil, ErrStateDirBusy
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (l *RootLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.once.Do(func() {
		l.err = errors.Join(unlockFile(l.file), l.file.Close())
	})
	return l.err
}
