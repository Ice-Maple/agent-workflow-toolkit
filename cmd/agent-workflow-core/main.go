package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	protocol "agent-workflow-toolkit/internal/protocol/v1"
)

const version = "0.1.0-wave0"

func main() {
	showVersion := flag.Bool("version", false, "print the core version")
	validateRequest := flag.Bool("validate-request", false, "validate one protocol v1 request from stdin")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if !*validateRequest {
		fmt.Fprintln(os.Stderr, "usage: agent-workflow-core --version | --validate-request")
		os.Exit(2)
	}

	request, err := protocol.DecodeRequest(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	response, err := protocol.NewSuccessResponse(request.RequestID, map[string]any{
		"status":    "protocol_valid",
		"operation": request.Operation,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
