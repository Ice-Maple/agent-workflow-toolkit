package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	args := os.Args[1:]
	if contains(args, "--version") || contains(args, "version") {
		fmt.Println("fixture-provider 1.0.0")
		return
	}
	time.Sleep(50 * time.Millisecond)
	switch {
	case contains(args, "--workspace"):
		fmt.Println(`{"type":"system","subtype":"init","session_id":"cursor-bridge-session"}`)
		time.Sleep(500 * time.Millisecond)
		fmt.Println(`{"type":"result","subtype":"success","is_error":false,"result":"ADAPTER_SMOKE_OK"}`)
	case contains(args, "--permission-mode"):
		fmt.Println(`{"type":"system","subtype":"init","session_id":"claude-bridge-session"}`)
		time.Sleep(500 * time.Millisecond)
		fmt.Println(`{"type":"assistant","session_id":"claude-bridge-session","message":{"content":[{"type":"text","text":"ADAPTER_SMOKE_OK"}]}}`)
		fmt.Println(`{"type":"result","subtype":"success","is_error":false,"result":"ADAPTER_SMOKE_OK","session_id":"claude-bridge-session"}`)
	default:
		fmt.Println(`{"type":"thread.started","thread_id":"codex-bridge-session"}`)
		time.Sleep(500 * time.Millisecond)
		fmt.Println(`{"type":"item.completed","item":{"type":"agent_message","text":"ADAPTER_SMOKE_OK"}}`)
		fmt.Println(`{"type":"turn.completed","status":"success"}`)
		if path := valueAfter(args, "--output-last-message"); path != "" {
			_ = os.WriteFile(path, []byte("ADAPTER_SMOKE_OK\n"), 0o600)
		}
	}
}

func contains(args []string, value string) bool {
	for _, arg := range args {
		if strings.EqualFold(arg, value) {
			return true
		}
	}
	return false
}

func valueAfter(args []string, name string) string {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == name {
			return args[index+1]
		}
	}
	return ""
}
