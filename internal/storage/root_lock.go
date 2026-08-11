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
	file, err := os.OpenFile(filepath.Join(root, ".gateway.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open state-dir lock: %w", err)
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
