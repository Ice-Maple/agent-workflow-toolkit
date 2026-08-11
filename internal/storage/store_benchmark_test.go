package storage

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"agent-workflow-toolkit/internal/orchestration"
)

func BenchmarkStoreEventAppend(b *testing.B) {
	path := filepath.Join(b.TempDir(), "events.jsonl")
	event := orchestration.Event{
		ID:        "event-benchmark",
		TaskID:    "task-benchmark",
		Sequence:  1,
		Type:      "benchmark.event",
		Data:      map[string]any{"actor": "benchmark", "status": "completed"},
		CreatedAt: time.Unix(0, 0).UTC(),
	}
	raw, err := json.Marshal(event)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(raw) + 1))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := appendJSONLine(path, event); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStoreSnapshotReplace(b *testing.B) {
	store, err := New(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	task := orchestration.Task{
		ID:                "task-benchmark",
		Objective:         "measure snapshot replacement",
		Workspace:         "benchmark-workspace",
		CheckpointMode:    "manual",
		Status:            orchestration.TaskActive,
		CurrentRevisionID: "revision-benchmark",
		CreatedAt:         time.Unix(0, 0).UTC(),
		UpdatedAt:         time.Unix(0, 0).UTC(),
	}
	revision := orchestration.TaskRevision{
		ID:          "revision-benchmark",
		TaskID:      task.ID,
		Instruction: "benchmark",
		CreatedAt:   time.Unix(0, 0).UTC(),
	}
	state := NewState(task, revision)
	for i := 0; i < 100; i++ {
		state.Events = append(state.Events, orchestration.Event{
			ID:        fmt.Sprintf("event-%03d", i),
			TaskID:    task.ID,
			Sequence:  i + 1,
			Type:      "benchmark.event",
			Data:      map[string]any{"actor": fmt.Sprintf("actor-%d", i%4)},
			CreatedAt: time.Unix(int64(i), 0).UTC(),
		})
	}
	raw, err := marshalSanitizedIndent(state)
	if err != nil {
		b.Fatal(err)
	}
	if err := store.Save(state); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(raw)))
	b.ReportMetric(float64(len(raw)), "snapshot_bytes/op")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := store.Save(state); err != nil {
			b.Fatal(err)
		}
	}
}
