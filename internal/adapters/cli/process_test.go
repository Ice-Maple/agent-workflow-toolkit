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
