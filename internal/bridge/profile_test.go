package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"agent-workflow-toolkit/internal/orchestration"
)

func TestHashProfileProducesLoadableCanonicalProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	raw := []byte(`{"id":"cursor-readonly","adapter":"cursor","executable":"cursor-agent.exe","trust":"authorized","mode":"plan","profile_hash":"stale"}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	profile, err := HashProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if profile.ProfileHash != orchestration.HashAdapterProfile(profile) {
		t.Fatalf("hashed profile = %#v", profile)
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ProfileHash != profile.ProfileHash {
		t.Fatalf("loaded hash = %q, want %q", loaded.ProfileHash, profile.ProfileHash)
	}
}
