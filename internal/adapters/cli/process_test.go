package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunnerCapturesOutputAndMergesEnvironment(t *testing.T) {
	result, err := NewRunner().Run(t.Context(), Command{
		Executable: os.Args[0],
		Args:       []string{},
		Dir:        t.TempDir(),
		Environment: []string{
			"CLI_PROCESS_HELPER=1",
			"CLI_PROCESS_HELPER_MODE=output",
			"CLI_PROCESS_VALUE=override",
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 || !result.Started {
		t.Fatalf("unexpected process result: %#v", result)
	}
	if !strings.Contains(string(result.Stdout), "CLI_OUTPUT_OK") || !strings.Contains(string(result.Stdout), "override") {
		t.Fatalf("stdout = %q", result.Stdout)
	}
}

func TestRunnerReportsContextTimeoutAndCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	result, err := NewRunner().Run(ctx, Command{
		Executable:  os.Args[0],
		Args:        []string{},
		Dir:         t.TempDir(),
		Environment: []string{"CLI_PROCESS_HELPER=1", "CLI_PROCESS_HELPER_MODE=sleep"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Started || !result.Cancelled || !result.TimedOut || !result.KillConfirmed {
		t.Fatalf("timeout evidence = %#v", result)
	}
}

func TestRunnerDrainsAllFastOutputLinesBeforeFinished(t *testing.T) {
	handle, err := NewRunner().Start(t.Context(), Command{
		Executable: os.Args[0],
		Dir:        t.TempDir(),
		Environment: []string{
			"CLI_PROCESS_HELPER=1",
			"CLI_PROCESS_HELPER_MODE=burst",
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	<-handle.Finished()
	lines := handle.DrainStdoutLines()
	result := handle.Wait()
	if result.ExitCode != 0 || len(lines) != 3 {
		t.Fatalf("process result=%#v lines=%q", result, lines)
	}
	if string(lines[0]) != "CLI_BURST_1" || string(lines[1]) != "CLI_BURST_2" || string(lines[2]) != "CLI_BURST_3" {
		t.Fatalf("captured lines = %q", lines)
	}
}

func TestRunnerFlushesUnterminatedOutputBeforeFinished(t *testing.T) {
	handle, err := NewRunner().Start(t.Context(), Command{
		Executable: os.Args[0],
		Dir:        t.TempDir(),
		Environment: []string{
			"CLI_PROCESS_HELPER=1",
			"CLI_PROCESS_HELPER_MODE=unterminated",
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	<-handle.Finished()
	lines := handle.DrainStdoutLines()
	result := handle.Wait()
	if result.ExitCode != 0 || len(lines) != 1 || string(lines[0]) != "CLI_UNTERMINATED" {
		t.Fatalf("process result=%#v lines=%q", result, lines)
	}
}

func TestRunnerFlushesUnterminatedStdoutAndStderrWithCR(t *testing.T) {
	handle, err := NewRunner().Start(t.Context(), Command{
		Executable: os.Args[0],
		Dir:        t.TempDir(),
		Environment: []string{
			"CLI_PROCESS_HELPER=1",
			"CLI_PROCESS_HELPER_MODE=mixed-unterminated",
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	<-handle.Finished()
	stdout := handle.DrainStdoutLines()
	stderr := handle.DrainStderrLines()
	result := handle.Wait()
	if result.ExitCode != 0 || len(stdout) != 1 || len(stderr) != 1 || string(stdout[0]) != "CLI_STDOUT_UNTERMINATED" || string(stderr[0]) != "CLI_STDERR_UNTERMINATED" {
		t.Fatalf("process result=%#v stdout=%q stderr=%q", result, stdout, stderr)
	}
}

func TestRunnerCapturesLongOutputWithoutTruncation(t *testing.T) {
	handle, err := NewRunner().Start(t.Context(), Command{
		Executable: os.Args[0],
		Dir:        t.TempDir(),
		Environment: []string{
			"CLI_PROCESS_HELPER=1",
			"CLI_PROCESS_HELPER_MODE=long",
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	result := handle.Wait()
	if result.ExitCode != 0 || len(result.Stdout) != 256*1024+1 || len(result.Stderr) != 128*1024+1 || !strings.HasSuffix(string(result.Stdout), "\n") || !strings.HasSuffix(string(result.Stderr), "\n") {
		t.Fatalf("long output result: exit=%d stdout=%d stderr=%d", result.ExitCode, len(result.Stdout), len(result.Stderr))
	}
}

func TestRunnerRepeatedCancelIsStable(t *testing.T) {
	killCalls := make(chan int, 1)
	runner := &Runner{KillTree: func(pid int) error {
		killCalls <- pid
		process, err := os.FindProcess(pid)
		if err != nil {
			return err
		}
		return process.Kill()
	}}
	handle, err := runner.Start(t.Context(), Command{
		Executable: os.Args[0],
		Dir:        t.TempDir(),
		Environment: []string{
			"CLI_PROCESS_HELPER=1",
			"CLI_PROCESS_HELPER_MODE=sleep",
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	first := handle.CancelWithCause(context.DeadlineExceeded)
	if !first.Requested || !first.Confirmed || first.AlreadyDone {
		t.Fatalf("first cancel = %#v", first)
	}
	result := handle.Wait()
	if !result.Cancelled || !result.TimedOut || !result.KillConfirmed {
		t.Fatalf("cancel evidence = %#v", result)
	}
	second := handle.Cancel()
	if !second.AlreadyDone || second.Requested || second.Confirmed {
		t.Fatalf("second cancel = %#v", second)
	}
	select {
	case pid := <-killCalls:
		if pid < 1 {
			t.Fatalf("kill pid = %d", pid)
		}
	default:
		t.Fatal("injected process-tree killer was not called")
	}
}

func TestRunnerSeparatesStartError(t *testing.T) {
	_, err := NewRunner().Run(t.Context(), Command{Executable: `Z:\missing\agent.exe`})
	if err == nil || !strings.Contains(err.Error(), "start external CLI") {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestMain(m *testing.M) {
	if os.Getenv("CLI_PROCESS_HELPER") == "1" {
		switch os.Getenv("CLI_PROCESS_HELPER_MODE") {
		case "output":
			fmt.Println("CLI_OUTPUT_OK " + os.Getenv("CLI_PROCESS_VALUE"))
		case "burst":
			fmt.Println("CLI_BURST_1")
			fmt.Println("CLI_BURST_2")
			fmt.Println("CLI_BURST_3")
		case "unterminated":
			_, _ = os.Stdout.Write([]byte("CLI_UNTERMINATED"))
		case "mixed-unterminated":
			_, _ = os.Stdout.Write([]byte("CLI_STDOUT_UNTERMINATED\r"))
			_, _ = os.Stderr.Write([]byte("CLI_STDERR_UNTERMINATED\r"))
		case "long":
			_, _ = os.Stdout.Write([]byte(strings.Repeat("O", 256*1024)))
			_, _ = os.Stdout.Write([]byte{'\n'})
			_, _ = os.Stderr.Write([]byte(strings.Repeat("E", 128*1024)))
			_, _ = os.Stderr.Write([]byte{'\n'})
		case "sleep":
			fmt.Println("CLI_SLEEP_STARTED")
			time.Sleep(30 * time.Second)
		default:
			fmt.Println("CLI_HELPER_UNKNOWN")
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
