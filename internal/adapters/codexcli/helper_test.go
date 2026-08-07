package codexcli

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("CODEX_ADAPTER_HELPER") == "1" {
		path := os.Getenv("CODEX_ADAPTER_LAST_MESSAGE")
		if path == "" {
			fmt.Fprintln(os.Stderr, "missing CODEX_ADAPTER_LAST_MESSAGE")
			os.Exit(2)
		}
		if err := os.WriteFile(path, []byte("ADAPTER_SMOKE_OK\n"), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		// Deliberately emit only a non-final JSONL event. The Adapter must use
		// the durable last-message file rather than treating this as a final.
		fmt.Println(`{"type":"turn.completed","status":"success"}`)
		os.Exit(0)
	}
	os.Exit(m.Run())
}
