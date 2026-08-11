package v1

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRuntimeOperationsMatchPublishedRequestSchema(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	schemaPath := filepath.Join(filepath.Dir(source), "..", "..", "..", "protocol", "v1", "request.schema.json")
	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties struct {
			Operation struct {
				Enum []string `json:"enum"`
			} `json:"operation"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	published := make(map[string]struct{}, len(schema.Properties.Operation.Enum))
	for _, operation := range schema.Properties.Operation.Enum {
		published[operation] = struct{}{}
	}
	if len(published) != len(operations) {
		t.Fatalf("schema operations=%v runtime operations=%v", published, operations)
	}
	for operation := range operations {
		if _, ok := published[operation]; !ok {
			t.Fatalf("runtime operation %q is absent from request schema", operation)
		}
	}
}

func TestDecodeRequestAcceptsValidEnvelope(t *testing.T) {
	req, err := DecodeRequest(strings.NewReader(`{"protocol_version":1,"request_id":"req-1","operation":"runPreflight","payload":{"node_id":"node-1"}}`))
	if err != nil {
		t.Fatalf("DecodeRequest() error = %v", err)
	}
	if req.RequestID != "req-1" || req.Operation != "runPreflight" {
		t.Fatalf("unexpected request: %#v", req)
	}
}

func TestDecodeRequestAcceptsRetryAndFallbackOperations(t *testing.T) {
	for _, operation := range []string{"prepareRetryAttempt", "prepareFallbackAttempt"} {
		request := `{"protocol_version":1,"request_id":"req-` + operation + `","operation":"` + operation + `","payload":{}}`
		if _, err := DecodeRequest(strings.NewReader(request)); err != nil {
			t.Fatalf("DecodeRequest(%s) error = %v", operation, err)
		}
	}
}

func TestDecodeRequestRejectsUnsupportedMajor(t *testing.T) {
	_, err := DecodeRequest(strings.NewReader(`{"protocol_version":2,"request_id":"req-1","operation":"runPreflight","payload":{}}`))
	assertProtocolCode(t, err, ErrProtocolVersion)
}

func TestDecodeRequestRejectsUnknownField(t *testing.T) {
	_, err := DecodeRequest(strings.NewReader(`{"protocol_version":1,"request_id":"req-1","operation":"runPreflight","payload":{},"secret":"must-not-pass"}`))
	assertProtocolCode(t, err, ErrMalformedJSON)
}

func TestDecodeRequestRejectsUnknownOperation(t *testing.T) {
	_, err := DecodeRequest(strings.NewReader(`{"protocol_version":1,"request_id":"req-1","operation":"inventedRole","payload":{}}`))
	assertProtocolCode(t, err, ErrUnknownOperation)
}

func TestDecodeRequestRejectsTrailingJSON(t *testing.T) {
	_, err := DecodeRequest(strings.NewReader(`{"protocol_version":1,"request_id":"req-1","operation":"runPreflight","payload":{}} {}`))
	assertProtocolCode(t, err, ErrTrailingJSON)
}

func TestDecodeRequestAcceptsPowerShellUTF8BOMTransportMarker(t *testing.T) {
	_, err := DecodeRequest(strings.NewReader("\ufeff{\"protocol_version\":1,\"request_id\":\"req-bom\",\"operation\":\"runPreflight\",\"payload\":{}}"))
	if err != nil {
		t.Fatalf("DecodeRequest() rejected a UTF-8 BOM transport marker: %v", err)
	}
}

func TestResponsesKeepSuccessAndErrorShapesSeparate(t *testing.T) {
	success, err := NewSuccessResponse("req-1", map[string]any{"status": "accepted"})
	if err != nil {
		t.Fatalf("NewSuccessResponse() error = %v", err)
	}
	if err := success.Validate(); err != nil {
		t.Fatalf("success.Validate() error = %v", err)
	}
	failure, err := NewErrorResponse("req-1", "preflight_blocked", "binding is not registered", map[string]any{"adapter": "fake"})
	if err != nil {
		t.Fatalf("NewErrorResponse() error = %v", err)
	}
	if err := failure.Validate(); err != nil {
		t.Fatalf("failure.Validate() error = %v", err)
	}
	if failure.OK || failure.Error == nil || failure.Payload != nil {
		t.Fatalf("unexpected error response: %#v", failure)
	}
}

func assertProtocolCode(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected protocol error %q", want)
	}
	var protocolErr *ProtocolError
	if !errors.As(err, &protocolErr) {
		t.Fatalf("error type = %T, want *ProtocolError: %v", err, err)
	}
	if protocolErr.Code != want {
		t.Fatalf("error code = %q, want %q", protocolErr.Code, want)
	}
}
