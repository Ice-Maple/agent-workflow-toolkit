package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"agent-workflow-toolkit/internal/gateway"
	"agent-workflow-toolkit/internal/policy"
	protocol "agent-workflow-toolkit/internal/protocol/v1"
	"agent-workflow-toolkit/internal/storage"
)

const version = "0.1.0-wave1"

func main() {
	showVersion := flag.Bool("version", false, "print the core version")
	validateRequest := flag.Bool("validate-request", false, "validate one protocol v1 request from stdin")
	gatewayMode := flag.Bool("gateway", false, "handle one protocol v1 request through the TaskGateway")
	stateDir := flag.String("state-dir", "", "directory for task state and request receipts")
	policyFile := flag.String("policy-file", "", "JSON policy file containing registered bindings")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if !*validateRequest && !*gatewayMode {
		fmt.Fprintln(os.Stderr, "usage: agent-workflow-core --version | --validate-request | --gateway [--state-dir DIR] [--policy-file FILE]")
		os.Exit(2)
	}

	request, err := protocol.DecodeRequest(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *validateRequest {
		writeSuccess(request.RequestID, map[string]any{
			"status":    "protocol_valid",
			"operation": request.Operation,
		})
		return
	}

	root := *stateDir
	if root == "" {
		configDir, configErr := os.UserConfigDir()
		if configErr != nil {
			fmt.Fprintln(os.Stderr, configErr)
			os.Exit(1)
		}
		root = filepath.Join(configDir, "agent-workflow-toolkit")
	}
	store, err := storage.New(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	config := policy.DefaultConfig()
	if *policyFile != "" {
		raw, readErr := os.ReadFile(*policyFile)
		if readErr != nil {
			fmt.Fprintln(os.Stderr, readErr)
			os.Exit(1)
		}
		config, err = policy.LoadJSON(raw)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	core, err := gateway.New(store, config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	payload, err := core.Handle(request)
	if err != nil {
		if gatewayErr, ok := err.(*gateway.Error); ok {
			response, responseErr := protocol.NewErrorResponse(request.RequestID, gatewayErr.Code, gatewayErr.Message, gatewayErr.Details)
			if responseErr != nil {
				fmt.Fprintln(os.Stderr, responseErr)
				os.Exit(1)
			}
			writeResponse(response)
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	writeSuccess(request.RequestID, payload)
}

func writeSuccess(requestID string, payload any) {
	response, err := protocol.NewSuccessResponse(requestID, payload)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	writeResponse(response)
}

func writeResponse(response protocol.Response) {
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
