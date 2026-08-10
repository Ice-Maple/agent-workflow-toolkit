// Package cli contains provider-independent process and stream primitives
// shared by external CLI adapters.
package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Command is an argv-only process invocation. The runner never evaluates the
// command through a shell, so prompts and paths remain data rather than shell
// syntax.
type Command struct {
	Executable  string
	Args        []string
	Dir         string
	Environment []string
	Stdin       io.Reader
}

func (c Command) Validate() error {
	if strings.TrimSpace(c.Executable) == "" {
		return errors.New("executable is required")
	}
	if c.Dir != "" {
		if !filepath.IsAbs(c.Dir) {
			return errors.New("process directory must be absolute")
		}
		info, err := os.Stat(c.Dir)
		if err != nil {
			return fmt.Errorf("process directory is not accessible: %w", err)
		}
		if !info.IsDir() {
			return errors.New("process directory must be a directory")
		}
	}
	return nil
}

// Result contains process evidence. A process start error is returned from
// Start/Run and is never represented as a provider result.
type Result struct {
	ExitCode      int
	Stdout        []byte
	Stderr        []byte
	Started       bool
	Cancelled     bool
	TimedOut      bool
	KillConfirmed bool
	Duration      time.Duration
}

// CancelResult describes the host's attempt to stop a process tree.
type CancelResult struct {
	Requested   bool
	Confirmed   bool
	AlreadyDone bool
	Err         error
}

// Runner starts argv-only processes and owns their output/cancellation
// lifecycle. It deliberately does not add provider-specific flags.
type Runner struct {
	KillTree func(pid int) error
}

func NewRunner() *Runner {
	return &Runner{KillTree: killProcessTree}
}

// Run starts a process, waits for it, and returns captured stdout/stderr.
func (r *Runner) Run(ctx context.Context, command Command) (Result, error) {
	handle, err := r.Start(ctx, command)
	if err != nil {
		return Result{}, err
	}
	return handle.Wait(), nil
}

// Handle is a started process. Output lines are retained internally and may
// be drained while the process is running; this prevents a slow adapter from
// blocking the provider's stdout pipe or losing transcript bytes.
type Handle struct {
	cmd       *exec.Cmd
	killTree  func(pid int) error
	startedAt time.Time

	mu              sync.Mutex
	stdout          bytes.Buffer
	stderr          bytes.Buffer
	stdoutLines     [][]byte
	stderrLines     [][]byte
	cancelCause     error
	cancelRequested bool
	killConfirmed   bool
	completed       bool

	lineNotify chan struct{}
	finishedCh chan struct{}
	done       chan Result
}

// Start launches a process without a shell. The supplied context remains
// attached to the process lifetime: cancellation requests a process-tree kill
// and is reported distinctly from an ordinary non-zero provider exit.
func (r *Runner) Start(ctx context.Context, command Command) (*Handle, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	if err := command.Validate(); err != nil {
		return nil, err
	}
	killTree := r.KillTree
	if killTree == nil {
		killTree = killProcessTree
	}
	cmd := exec.Command(command.Executable, command.Args...)
	configureCommand(cmd)
	cmd.Dir = command.Dir
	cmd.Stdin = command.Stdin
	if command.Environment != nil {
		cmd.Env = mergedEnvironment(command.Environment)
	}
	handle := &Handle{
		cmd:        cmd,
		killTree:   killTree,
		startedAt:  time.Now(),
		lineNotify: make(chan struct{}, 1),
		finishedCh: make(chan struct{}),
		done:       make(chan Result, 1),
	}
	stdout := &outputCollector{handle: handle, stdout: true}
	stderr := &outputCollector{handle: handle}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start external CLI: %w", err)
	}
	go func() {
		err := cmd.Wait()
		// When Stdout/Stderr are custom writers, os/exec waits for its copy
		// goroutines before returning from Wait. Flush a final line without a
		// newline before publishing Finished/Done to observers.
		stdout.flush()
		stderr.flush()
		handle.finish(err)
	}()
	go func() {
		select {
		case <-ctx.Done():
			handle.requestCancel(ctx.Err())
		case <-handle.finishedCh:
		}
	}()
	return handle, nil
}

type outputCollector struct {
	handle  *Handle
	stdout  bool
	pending []byte
}

func (c *outputCollector) Write(data []byte) (int, error) {
	if c == nil || c.handle == nil {
		return 0, errors.New("output collector is not initialized")
	}
	if len(data) == 0 {
		return 0, nil
	}
	h := c.handle
	h.mu.Lock()
	if c.stdout {
		_, _ = h.stdout.Write(data)
	} else {
		_, _ = h.stderr.Write(data)
	}
	c.pending = append(c.pending, data...)
	notified := c.appendCompleteLinesLocked()
	h.mu.Unlock()
	if notified {
		select {
		case h.lineNotify <- struct{}{}:
		default:
		}
	}
	return len(data), nil
}

func (c *outputCollector) appendCompleteLinesLocked() bool {
	notified := false
	for {
		index := bytes.IndexByte(c.pending, '\n')
		if index < 0 {
			return notified
		}
		line := append([]byte(nil), c.pending[:index]...)
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if c.stdout {
			c.handle.stdoutLines = append(c.handle.stdoutLines, line)
		} else {
			c.handle.stderrLines = append(c.handle.stderrLines, line)
		}
		c.pending = c.pending[index+1:]
		notified = true
	}
}

func (c *outputCollector) flush() {
	if c == nil || c.handle == nil {
		return
	}
	h := c.handle
	h.mu.Lock()
	if len(c.pending) == 0 {
		h.mu.Unlock()
		return
	}
	line := append([]byte(nil), c.pending...)
	line = bytes.TrimSuffix(line, []byte{'\r'})
	if c.stdout {
		h.stdoutLines = append(h.stdoutLines, line)
	} else {
		h.stderrLines = append(h.stderrLines, line)
	}
	c.pending = nil
	h.mu.Unlock()
	select {
	case h.lineNotify <- struct{}{}:
	default:
	}
}

func (h *Handle) finish(waitErr error) {
	h.mu.Lock()
	if h.completed {
		h.mu.Unlock()
		return
	}
	result := Result{
		ExitCode: exitCode(waitErr),
		Started:  true,
		Duration: time.Since(h.startedAt),
		Stdout:   append([]byte(nil), h.stdout.Bytes()...),
		Stderr:   append([]byte(nil), h.stderr.Bytes()...),
	}
	result.Cancelled = h.cancelRequested
	// Once cancellation was explicitly requested, a process that reaches
	// Wait completion is no longer an orphan. This also closes the race where
	// taskkill/Process.Kill returns just after Wait snapshots the result.
	result.KillConfirmed = h.killConfirmed || h.cancelRequested
	if errors.Is(h.cancelCause, context.DeadlineExceeded) {
		result.TimedOut = true
	}
	h.completed = true
	close(h.finishedCh)
	h.mu.Unlock()
	h.done <- result
}

func (h *Handle) requestCancel(cause error) CancelResult {
	h.mu.Lock()
	if h.completed {
		h.mu.Unlock()
		return CancelResult{AlreadyDone: true}
	}
	h.cancelRequested = true
	h.cancelCause = cause
	killTree := h.killTree
	pid := h.cmd.Process.Pid
	h.mu.Unlock()
	err := killTree(pid)
	if err != nil {
		// The tree command may be unavailable in a constrained environment. A
		// direct kill is still safer than leaving an orphaned provider process.
		err = h.cmd.Process.Kill()
	}
	if err != nil {
		// A successful tree kill can race with Process.Kill/Wait and make the
		// second operation report "already exited". If the process has now
		// finished after our explicit request, that is confirmed cancellation,
		// not an unknown provider outcome.
		select {
		case <-h.finishedCh:
			err = nil
		case <-time.After(2 * time.Second):
		}
	}
	h.mu.Lock()
	if err == nil {
		h.killConfirmed = true
	}
	h.mu.Unlock()
	return CancelResult{Requested: true, Confirmed: err == nil, Err: err}
}

// CancelWithCause requests a process-tree cancellation and preserves the
// cause in the process evidence. It does not hide an unconfirmed kill behind
// a successful Go call.
func (h *Handle) CancelWithCause(cause error) CancelResult {
	if cause == nil {
		cause = context.Canceled
	}
	return h.requestCancel(cause)
}

// Cancel requests an ordinary process-tree cancellation.
func (h *Handle) Cancel() CancelResult {
	return h.CancelWithCause(context.Canceled)
}

// Wait blocks until the process and its output readers have completed.
func (h *Handle) Wait() Result {
	return <-h.done
}

func (h *Handle) Done() <-chan Result { return h.done }

// Finished is closed exactly once after process output readers have drained.
// It lets observers wait for completion without consuming the Result channel.
func (h *Handle) Finished() <-chan struct{} { return h.finishedCh }

// WaitForOutput wakes when new stdout/stderr is available or the process has
// finished. It is useful to wait for a provider session event without polling.
func (h *Handle) WaitForOutput(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	select {
	case <-h.lineNotify:
		return nil
	case <-h.finishedCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Handle) DrainStdoutLines() [][]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	lines := make([][]byte, len(h.stdoutLines))
	for index, line := range h.stdoutLines {
		lines[index] = append([]byte(nil), line...)
	}
	h.stdoutLines = nil
	return lines
}

func (h *Handle) DrainStderrLines() [][]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	lines := make([][]byte, len(h.stderrLines))
	for index, line := range h.stderrLines {
		lines[index] = append([]byte(nil), line...)
	}
	h.stderrLines = nil
	return lines
}

func (h *Handle) Stdout() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]byte(nil), h.stdout.Bytes()...)
}

func (h *Handle) Stderr() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]byte(nil), h.stderr.Bytes()...)
}

func (h *Handle) PID() int {
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return 0
	}
	return h.cmd.Process.Pid
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func mergedEnvironment(overrides []string) []string {
	values := make(map[string]string)
	order := make([]string, 0)
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		normalizedKey := environmentKey(key)
		values[normalizedKey] = item
		order = append(order, normalizedKey)
	}
	for _, item := range overrides {
		key, _, ok := strings.Cut(item, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		normalizedKey := environmentKey(key)
		if _, exists := values[normalizedKey]; !exists {
			order = append(order, normalizedKey)
		}
		values[normalizedKey] = item
	}
	result := make([]string, 0, len(order))
	for _, key := range order {
		if value, ok := values[key]; ok {
			result = append(result, value)
		}
	}
	return result
}

func environmentKey(key string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(key)
	}
	return key
}

func killProcessTree(pid int) error {
	if pid < 1 {
		return errors.New("process id is required")
	}
	if runtime.GOOS == "windows" {
		taskkill, err := exec.LookPath("taskkill")
		if err != nil {
			return err
		}
		command := exec.Command(taskkill, "/PID", strconv.Itoa(pid), "/T", "/F")
		configureCommand(command)
		return command.Run()
	}
	return errors.New("process-tree kill is not available for this platform")
}
