package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"agent-workflow-toolkit/internal/gateway"
	"agent-workflow-toolkit/internal/policy"
	protocol "agent-workflow-toolkit/internal/protocol/v1"
	"agent-workflow-toolkit/internal/storage"
)

const (
	version  = "0.2.0"
	maturity = "experimental"
)

type versionMetadata struct {
	Version       string `json:"version"`
	ProtocolMajor int    `json:"protocol_major"`
	Maturity      string `json:"maturity"`
	Revision      string `json:"revision"`
	Dirty         bool   `json:"dirty"`
}

func main() {
	showVersion := flag.Bool("version", false, "print the core version")
	validateRequest := flag.Bool("validate-request", false, "validate one protocol v1 envelope from stdin")
	gatewayMode := flag.Bool("gateway", false, "handle one protocol v1 request through the TaskGateway")
	stateDir := flag.String("state-dir", "", "directory for task state and request receipts")
	policyFile := flag.String("policy-file", "", "JSON policy file containing registered bindings")
	lockTimeout := flag.Duration("lock-timeout", 5*time.Second, "maximum time to wait for exclusive state-dir access")
	flag.Parse()

	if *showVersion {
		metadata := currentVersionMetadata()
		fmt.Printf("agent-workflow-core version=%s protocol=%d maturity=%s revision=%s dirty=%t\n", metadata.Version, metadata.ProtocolMajor, metadata.Maturity, metadata.Revision, metadata.Dirty)
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
			"status":    "envelope_valid",
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
	rootLock, err := storage.AcquireRootLock(root, *lockTimeout)
	if err != nil {
		if errors.Is(err, storage.ErrStateDirBusy) {
			writeError(request.RequestID, "state_dir_busy", "another gateway process owns this state-dir", nil)
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() {
		if releaseErr := rootLock.Release(); releaseErr != nil {
			fmt.Fprintln(os.Stderr, releaseErr)
		}
	}()
	store, err := storage.New(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
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

func currentVersionMetadata() versionMetadata {
	metadata := versionMetadata{Version: version, ProtocolMajor: protocol.Major, Maturity: maturity, Revision: "unknown"}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return metadata
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if strings.TrimSpace(setting.Value) != "" {
				metadata.Revision = setting.Value
			}
		case "vcs.modified":
			metadata.Dirty = setting.Value == "true"
		}
	}
	return metadata
}

func writeSuccess(requestID string, payload any) {
	response, err := protocol.NewSuccessResponse(requestID, payload)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	writeResponse(response)
}

func writeError(requestID, code, message string, details map[string]any) {
	response, err := protocol.NewErrorResponse(requestID, code, message, details)
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
