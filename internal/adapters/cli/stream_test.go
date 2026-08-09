package cli

import "testing"

func TestStreamSummaryRejectsToolOnlyTurnAsDelivery(t *testing.T) {
	summary := ParseJSONL([]byte("{" +
		`"type":"system","session_id":"cursor-session-1"` +
		"}\n{" +
		`"type":"tool_call","name":"CreatePlan","input":{"text":"report"}` +
		"}\n{" +
		`"type":"turn_ended","status":"success"` +
		"}"))
	if summary.SessionID != "cursor-session-1" || !summary.Terminal || summary.FinalText != "" || summary.DeliveryComplete() {
		t.Fatalf("tool-only summary = %#v", summary)
	}
}

func TestStreamSummaryExtractsFinalResultAndTerminalEvidence(t *testing.T) {
	summary := ParseJSONL([]byte("{" +
		`"type":"system","subtype":"init","session_id":"cursor-session-2"` +
		"}\n{" +
		`"type":"result","subtype":"success","is_error":false,"result":"ADAPTER_SMOKE_OK"` +
		"}"))
	if !summary.DeliveryComplete() || summary.FinalText != "ADAPTER_SMOKE_OK" || summary.FinalSource != "result.result" {
		t.Fatalf("final summary = %#v", summary)
	}
}

func TestStreamSummaryDoesNotUseToolUseAsFinalText(t *testing.T) {
	summary := ParseJSONL([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"CreatePlan"}]}}` + "\n" + `{"type":"turn_ended","status":"success"}`))
	if summary.FinalText != "" || summary.DeliveryComplete() {
		t.Fatalf("tool-use summary = %#v", summary)
	}
}

func TestStreamSummaryDoesNotTreatGenericNameAsTool(t *testing.T) {
	summary := ParseJSONL([]byte(`{"type":"assistant","name":"reviewer","text":"REPORT_OK"}` + "\n" + `{"type":"turn_ended","status":"success"}`))
	if !summary.DeliveryComplete() || summary.FinalText != "REPORT_OK" || summary.FinalSource != "assistant.text" {
		t.Fatalf("named assistant summary = %#v", summary)
	}
}

func TestStreamSummaryPreservesMalformedLinesWithoutCompletion(t *testing.T) {
	summary := ParseJSONL([]byte("provider diagnostic\n{" + `"type":"turn_ended","status":"success"` + "}"))
	if !summary.Terminal || summary.DeliveryComplete() || len(summary.Raw) == 0 {
		t.Fatalf("malformed-line summary = %#v", summary)
	}
}
