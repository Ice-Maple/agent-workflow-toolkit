package v1

import (
	"errors"
	"strings"
	"testing"
)

func TestDecodeRequestAcceptsValidEnvelope(t *testing.T) {
	req, err := DecodeRequest(strings.NewReader(`{"protocol_version":1,"request_id":"req-1","operation":"runPreflight","payload":{"node_id":"node-1"}}`))
	if err != nil {
		t.Fatalf("DecodeRequest() error = %v", err)
	}
	if req.RequestID != "req-1" || req.Operation != "runPreflight" {
		t.Fatalf("unexpected request: %#v", req)
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
