package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-workflow-toolkit/internal/orchestration"
)

func TestSnapshotAndAppendOnlyEventReplay(t *testing.T) {
	root := t.TempDir()
	store, err := New(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := orchestration.NewTask("storage", "persist events", root)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := orchestration.NewRevision(task.ID, "", task.Objective)
	if err != nil {
		t.Fatal(err)
	}
	task.CurrentRevisionID = revision.ID
	state := NewState(task, revision)
	first := orchestration.NewEvent(task.ID, "task.created", nil)
	state.Events = append(state.Events, first)
	if err := store.SaveWithEvent(state, first); err != nil {
		t.Fatal(err)
	}
	second := orchestration.NewEvent(task.ID, "checkpoint.waiting", map[string]any{"node": "review"})
	state.Events = append(state.Events, second)
	if err := store.SaveWithEvent(state, second); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Events) != 2 {
		t.Fatalf("loaded events = %d, want 2", len(loaded.Events))
	}
	replayed, err := ReplayEvents(filepath.Join(root, "state", "tasks", task.ID, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 2 || replayed[0].ID != first.ID || replayed[1].ID != second.ID {
		t.Fatalf("replayed events = %#v", replayed)
	}
}

func TestStorageFiltersSecretsFromEventsAndReceipts(t *testing.T) {
	root := t.TempDir()
	store, err := New(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := orchestration.NewTask("storage", "filter secrets", root)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := orchestration.NewRevision(task.ID, "", task.Objective)
	if err != nil {
		t.Fatal(err)
	}
	task.CurrentRevisionID = revision.ID
	state := NewState(task, revision)
	event := orchestration.NewEvent(task.ID, "diagnostic", map[string]any{
		"api_token":     "do-not-persist-this-token",
		"Authorization": "Bearer do-not-persist-this-header",
		"safe":          "kept",
		"nested":        map[string]any{"client_secret": "do-not-persist-this-secret"},
		"content":       `{"token":"do-not-persist-this-embedded-token"}`,
	})
	if err := store.SaveWithEvent(state, event); err != nil {
		t.Fatal(err)
	}
	receiptPayload := json.RawMessage(`{"status":"ok","authorization":"Bearer do-not-persist-this-receipt-token"}`)
	if err := store.SaveReceipt(RequestReceipt{RequestID: "request-1", Operation: "test", PayloadHash: "hash", Payload: receiptPayload}); err != nil {
		t.Fatal(err)
	}
	snapshotRaw, err := os.ReadFile(filepath.Join(root, "state", "tasks", task.ID, "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	eventsRaw, err := os.ReadFile(filepath.Join(root, "state", "tasks", task.ID, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	receiptRaw, err := os.ReadFile(filepath.Join(root, "state", "requests", hashKey("request-1")+".json"))
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"snapshot": snapshotRaw, "events": eventsRaw, "receipt": receiptRaw} {
		text := string(raw)
		if strings.Contains(text, "do-not-persist") {
			t.Fatalf("%s persisted a secret: %s", name, text)
		}
		if !strings.Contains(text, "[REDACTED]") {
			t.Fatalf("%s did not contain a redaction marker: %s", name, text)
		}
	}
}

func TestInterruptedAtomicReplacementRecoversOldSnapshot(t *testing.T) {
	root := t.TempDir()
	stateRoot := filepath.Join(root, "state")
	store, err := New(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	task, err := orchestration.NewTask("storage", "recover snapshot", root)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := orchestration.NewRevision(task.ID, "", task.Objective)
	if err != nil {
		t.Fatal(err)
	}
	task.CurrentRevisionID = revision.ID
	if err := store.Save(NewState(task, revision)); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(stateRoot, "tasks", task.ID, "snapshot.json")
	backup := snapshot + ".bak"
	temp := filepath.Join(filepath.Dir(snapshot), ".atomic-interrupted")
	if err := os.Rename(snapshot, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temp, []byte(`{"not":"the durable snapshot"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJournal(snapshot+".journal", atomicJournal{Target: snapshot, Temp: temp, Backup: backup, Phase: "backup_created"}); err != nil {
		t.Fatal(err)
	}
	if _, err := New(stateRoot); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Task.ID != task.ID || recovered.Task.CurrentRevisionID != revision.ID {
		t.Fatalf("recovered snapshot changed: %#v", recovered.Task)
	}
	for _, path := range []string{snapshot + ".journal", backup, temp} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("recovery left temporary file %q (err=%v)", path, err)
		}
	}
}

func TestLoadReconcilesSnapshotAndEventLogCrashWindows(t *testing.T) {
	root := t.TempDir()
	store, err := New(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := orchestration.NewTask("storage", "reconcile events", root)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := orchestration.NewRevision(task.ID, "", task.Objective)
	if err != nil {
		t.Fatal(err)
	}
	task.CurrentRevisionID = revision.ID
	state := NewState(task, revision)
	first := orchestration.NewEvent(task.ID, "first", nil)
	state.Events = append(state.Events, first)
	if err := store.SaveWithEvent(state, first); err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(root, "state", "tasks", task.ID, "events.jsonl")
	if err := os.Remove(eventPath); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Events) != 1 || loaded.Events[0].ID != first.ID {
		t.Fatalf("snapshot-ahead recovery = %#v", loaded.Events)
	}
	second := orchestration.NewEvent(task.ID, "second", nil)
	if err := appendJSONLine(eventPath, second); err != nil {
		t.Fatal(err)
	}
	reconciled, err := store.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reconciled.Events) != 2 || reconciled.Events[1].ID != second.ID {
		t.Fatalf("event-log-ahead recovery = %#v", reconciled.Events)
	}
	snapshotRaw, err := os.ReadFile(filepath.Join(root, "state", "tasks", task.ID, "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(snapshotRaw), second.ID) {
		t.Fatal("event-log-ahead recovery did not persist the merged snapshot")
	}
}
