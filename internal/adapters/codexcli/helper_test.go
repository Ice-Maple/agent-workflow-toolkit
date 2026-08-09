package codexcli

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("CODEX_ADAPTER_HELPER") == "1" {
		if hasCodexArg("--version") {
			fmt.Println("codex 0.147.0")
			os.Exit(0)
		}
		path := codexArgValue("--output-last-message")
		if path == "" {
			path = os.Getenv("CODEX_ADAPTER_LAST_MESSAGE")
		}
		mode := os.Getenv("CODEX_ADAPTER_HELPER_MODE")
		if mode == "no-session-sleep" {
			for {
				time.Sleep(time.Second)
			}
		}
		if mode == "exit-failure" {
			fmt.Fprintln(os.Stderr, "fixture failure")
			os.Exit(7)
		}
		sessionID := "codex-thread-1"
		if mode == "mismatched-session" && hasCodexArg("resume") {
			sessionID = "codex-foreign-thread"
		}
		if mode != "no-session" {
			fmt.Printf(`{"type":"thread.started","thread_id":%q}`+"\n", sessionID)
		}
		if mode == "session-sleep" {
			for {
				time.Sleep(time.Second)
			}
		}
		if mode == "tool-only" {
			fmt.Println(`{"type":"item.completed","item":{"type":"tool_call","name":"Read"}}`)
			fmt.Println(`{"type":"turn.completed","status":"success"}`)
			os.Exit(0)
		}
		if mode == "empty-delivery" {
			fmt.Println(`{"type":"turn.completed","status":"success"}`)
			os.Exit(0)
		}
		if mode == "no-session" {
			fmt.Println(`{"type":"turn.completed","status":"success"}`)
			os.Exit(0)
		}
		finalText := os.Getenv("CODEX_ADAPTER_HELPER_FINAL")
		if strings.TrimSpace(finalText) == "" {
			finalText = "ADAPTER_SMOKE_OK"
		}
		fmt.Printf(`{"type":"item.completed","item":{"type":"agent_message","text":%q}}`+"\n", finalText)
		fmt.Println(`{"type":"turn.completed","status":"success"}`)
		if path != "" {
			if err := os.WriteFile(path, []byte(finalText+"\n"), 0o600); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(3)
			}
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func hasCodexArg(want string) bool {
	for _, arg := range os.Args[1:] {
		if arg == want {
			return true
		}
	}
	return false
}

func codexArgValue(flag string) string {
	for index, arg := range os.Args[1:] {
		if arg == flag && index+2 <= len(os.Args[1:]) {
			return os.Args[index+2]
		}
	}
	return ""
}
