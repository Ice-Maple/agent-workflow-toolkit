package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkRunnerColdStart(b *testing.B) {
	workspace := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := NewRunner().Run(b.Context(), helperBenchmarkCommand(workspace, "output", "cold-start"))
		if err != nil {
			b.Fatal(err)
		}
		if result.ExitCode != 0 || strings.TrimSpace(string(result.Stdout)) != "CLI_OUTPUT_OK cold-start" {
			b.Fatalf("cold-start result = %#v", result)
		}
	}
}

func BenchmarkRunnerConcurrentActorIsolation(b *testing.B) {
	root := b.TempDir()
	var actorCount atomic.Int64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		actorID := actorCount.Add(1)
		workspace := filepath.Join(root, fmt.Sprintf("actor-%d", actorID))
		if err := os.MkdirAll(workspace, 0o700); err != nil {
			b.Errorf("create actor workspace: %v", err)
			return
		}
		value := fmt.Sprintf("actor-%d", actorID)
		for pb.Next() {
			result, err := NewRunner().Run(b.Context(), helperBenchmarkCommand(workspace, "output", value))
			if err != nil {
				b.Errorf("actor %d: %v", actorID, err)
				return
			}
			if result.ExitCode != 0 || strings.TrimSpace(string(result.Stdout)) != "CLI_OUTPUT_OK "+value {
				b.Errorf("actor %d isolation result = %#v", actorID, result)
				return
			}
		}
	})
	b.ReportMetric(float64(actorCount.Load()), "parallel_actors")
}

func BenchmarkRunnerCancellationResponse(b *testing.B) {
	workspace := b.TempDir()
	var total time.Duration
	b.ReportAllocs()
	b.StopTimer()
	for i := 0; i < b.N; i++ {
		handle, err := NewRunner().Start(b.Context(), helperBenchmarkCommand(workspace, "sleep", ""))
		if err != nil {
			b.Fatal(err)
		}
		waitForBenchmarkLine(b, handle, "CLI_SLEEP_STARTED")

		b.StartTimer()
		started := time.Now()
		cancel := handle.Cancel()
		result := handle.Wait()
		elapsed := time.Since(started)
		b.StopTimer()

		if cancel.Err != nil || !cancel.Requested || !cancel.Confirmed {
			b.Fatalf("cancel result = %#v", cancel)
		}
		if !result.Cancelled || !result.KillConfirmed {
			b.Fatalf("cancelled process result = %#v", result)
		}
		total += elapsed
	}
	if b.N > 0 {
		b.ReportMetric(float64(total.Nanoseconds())/float64(b.N), "cancel_ns/op")
	}
}

func helperBenchmarkCommand(workspace, mode, value string) Command {
	environment := []string{
		"CLI_PROCESS_HELPER=1",
		"CLI_PROCESS_HELPER_MODE=" + mode,
	}
	if value != "" {
		environment = append(environment, "CLI_PROCESS_VALUE="+value)
	}
	return Command{
		Executable:  os.Args[0],
		Dir:         workspace,
		Environment: environment,
	}
}

func waitForBenchmarkLine(b *testing.B, handle *Handle, want string) {
	b.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		for _, line := range handle.DrainStdoutLines() {
			if strings.TrimSpace(string(line)) == want {
				return
			}
		}
		if err := handle.WaitForOutput(ctx); err != nil {
			_ = handle.Cancel()
			_ = handle.Wait()
			b.Fatalf("wait for %q: %v", want, err)
		}
		select {
		case <-handle.Finished():
			result := handle.Wait()
			b.Fatalf("process finished before %q: %#v", want, result)
		default:
		}
	}
}
