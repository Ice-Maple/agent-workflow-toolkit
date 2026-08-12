// Package v1 implements the versioned local JSON contract between a
// Controller Skill and the shared Go core. It deliberately validates only
// protocol shape and operation names; task/provider semantics belong to the
// gateway and policy packages introduced in later Waves.
package v1

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const Major = 1

var operations = map[string]struct{}{
	"createTask":                          {},
	"proposeNode":                         {},
	"runPreflight":                        {},
	"prepareAttempt":                      {},
	"resolveRole":                         {},
	"createCollaborationRun":              {},
	"recordCollaborationTechnicalRelease": {},
	"prepareRetryAttempt":                 {},
	"prepareFallbackAttempt":              {},
	"pollHostActions":                     {},
	"getAttempt":                          {},
	"submitHostActionResult":              {},
	"bindProviderHandle":                  {},
	"appendEvent":                         {},
	"requestCompletion":                   {},
	"enableGoalMode":                      {},
	"revokeGoalMode":                      {},
	"recordHumanDecision":                 {},
	"requestCancel":                       {},
}

type ProtocolError struct {
	Code    string
	Field   string
	Message string
}

func (e *ProtocolError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("%s (%s): %s", e.Code, e.Field, e.Message)
}

const (
	ErrMalformedJSON        = "malformed_json"
	ErrTrailingJSON         = "trailing_json"
	ErrProtocolVersion      = "protocol_incompatible_major"
	ErrMissingField         = "missing_field"
	ErrInvalidField         = "invalid_field"
	ErrUnknownOperation     = "unknown_operation"
	ErrInvalidPayload       = "invalid_payload"
	ErrResponseInconsistent = "response_inconsistent"
)

type Request struct {
	ProtocolVersion int             `json:"protocol_version"`
	RequestID       string          `json:"request_id"`
	Operation       string          `json:"operation"`
	Payload         json.RawMessage `json:"payload"`
}

type Response struct {
	ProtocolVersion int             `json:"protocol_version"`
	RequestID       string          `json:"request_id"`
	OK              bool            `json:"ok"`
	Payload         json.RawMessage `json:"payload,omitempty"`
	Error           *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func NewRequest(requestID, operation string, payload any) (Request, error) {
	raw, err := marshalObject(payload)
	if err != nil {
		return Request{}, err
	}
	req := Request{ProtocolVersion: Major, RequestID: requestID, Operation: operation, Payload: raw}
	if err := req.Validate(); err != nil {
		return Request{}, err
	}
	return req, nil
}

func NewSuccessResponse(requestID string, payload any) (Response, error) {
	raw, err := marshalObject(payload)
	if err != nil {
		return Response{}, err
	}
	resp := Response{ProtocolVersion: Major, RequestID: requestID, OK: true, Payload: raw}
	if err := resp.Validate(); err != nil {
		return Response{}, err
	}
	return resp, nil
}

func NewErrorResponse(requestID, code, message string, details map[string]any) (Response, error) {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(message) == "" {
		return Response{}, &ProtocolError{Code: ErrInvalidField, Field: "error", Message: "code and message are required"}
	}
	resp := Response{
		ProtocolVersion: Major,
		RequestID:       requestID,
		OK:              false,
		Error:           &Error{Code: code, Message: message, Details: details},
	}
	if err := resp.Validate(); err != nil {
		return Response{}, err
	}
	return resp, nil
}

func DecodeRequest(r io.Reader) (Request, error) {
	var req Request
	if err := decodeOne(r, &req); err != nil {
		return Request{}, err
	}
	if err := req.Validate(); err != nil {
		return Request{}, err
	}
	return req, nil
}

func DecodeResponse(r io.Reader) (Response, error) {
	var resp Response
	if err := decodeOne(r, &resp); err != nil {
		return Response{}, err
	}
	if err := resp.Validate(); err != nil {
		return Response{}, err
	}
	return resp, nil
}

func (r Request) Validate() error {
	if r.ProtocolVersion != Major {
		return &ProtocolError{Code: ErrProtocolVersion, Field: "protocol_version", Message: fmt.Sprintf("got %d, supported major is %d", r.ProtocolVersion, Major)}
	}
	if strings.TrimSpace(r.RequestID) == "" {
		return &ProtocolError{Code: ErrMissingField, Field: "request_id", Message: "request_id must be non-empty"}
	}
	if _, ok := operations[r.Operation]; !ok {
		return &ProtocolError{Code: ErrUnknownOperation, Field: "operation", Message: r.Operation}
	}
	if !isJSONObject(r.Payload) {
		return &ProtocolError{Code: ErrInvalidPayload, Field: "payload", Message: "payload must be a JSON object"}
	}
	return nil
}

func (r Response) Validate() error {
	if r.ProtocolVersion != Major {
		return &ProtocolError{Code: ErrProtocolVersion, Field: "protocol_version", Message: fmt.Sprintf("got %d, supported major is %d", r.ProtocolVersion, Major)}
	}
	if strings.TrimSpace(r.RequestID) == "" {
		return &ProtocolError{Code: ErrMissingField, Field: "request_id", Message: "request_id must be non-empty"}
	}
	if r.OK {
		if r.Error != nil || !isJSONObject(r.Payload) {
			return &ProtocolError{Code: ErrResponseInconsistent, Field: "response", Message: "successful response requires an object payload and no error"}
		}
		return nil
	}
	if r.Error == nil || strings.TrimSpace(r.Error.Code) == "" || strings.TrimSpace(r.Error.Message) == "" || r.Payload != nil {
		return &ProtocolError{Code: ErrResponseInconsistent, Field: "response", Message: "error response requires code/message and no payload"}
	}
	return nil
}

func marshalObject(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, &ProtocolError{Code: ErrInvalidPayload, Field: "payload", Message: "payload must be a non-nil JSON object"}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, &ProtocolError{Code: ErrInvalidPayload, Field: "payload", Message: err.Error()}
	}
	if !isJSONObject(raw) {
		return nil, &ProtocolError{Code: ErrInvalidPayload, Field: "payload", Message: "payload must be a JSON object"}
	}
	return raw, nil
}

func isJSONObject(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' || !json.Valid(trimmed) {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(trimmed, &object) == nil
}

func decodeOne(r io.Reader, target any) error {
	// Windows PowerShell 5.1 can prepend an UTF-8 BOM when piping text into a
	// native process. Accept that transport marker, while keeping the JSON
	// envelope itself strict.
	buffered := bufio.NewReader(r)
	if prefix, err := buffered.Peek(3); err == nil && bytes.Equal(prefix, []byte{0xef, 0xbb, 0xbf}) {
		_, _ = buffered.Discard(3)
	}
	decoder := json.NewDecoder(buffered)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return &ProtocolError{Code: ErrMalformedJSON, Message: "empty JSON input"}
		}
		return &ProtocolError{Code: ErrMalformedJSON, Message: err.Error()}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return &ProtocolError{Code: ErrTrailingJSON, Message: "more than one JSON value"}
		}
		return &ProtocolError{Code: ErrTrailingJSON, Message: err.Error()}
	}
	return nil
}
