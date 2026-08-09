package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// StreamEvent is a provider-neutral view of one JSONL event. Raw is retained
// only in the in-memory summary; evidence writers should store references and
// sanitized metadata instead of credentials or complete environments.
type StreamEvent struct {
	Sequence       int
	Type           string
	SessionID      string
	Text           string
	TextSource     string
	Tool           bool
	Terminal       bool
	TerminalStatus string
}

type StreamSummary struct {
	SessionID      string
	Events         []StreamEvent
	FinalText      string
	FinalSource    string
	Terminal       bool
	TerminalStatus string
	Raw            []byte
}

func (s *StreamSummary) AddLine(line []byte) error {
	if s == nil {
		return fmt.Errorf("stream summary is nil")
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil
	}
	s.Raw = append(s.Raw, line...)
	s.Raw = append(s.Raw, '\n')
	var object map[string]any
	if err := json.Unmarshal(line, &object); err != nil {
		// A provider may place human diagnostics on stdout. Preserve the bytes
		// but do not treat them as structured completion evidence.
		return nil
	}
	event := normalizeEvent(object, len(s.Events)+1)
	s.Events = append(s.Events, event)
	if event.SessionID != "" && s.SessionID == "" {
		s.SessionID = event.SessionID
	}
	if event.Text != "" && !event.Tool {
		s.FinalText = strings.TrimSpace(event.Text)
		s.FinalSource = event.TextSource
	}
	if event.Terminal {
		s.Terminal = true
		s.TerminalStatus = event.TerminalStatus
	}
	return nil
}

func (s StreamSummary) DeliveryComplete() bool {
	if !s.Terminal || strings.TrimSpace(s.FinalText) == "" {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(s.TerminalStatus))
	return status == "" || status == "success" || status == "completed" || status == "complete" || status == "ok"
}

func ParseJSONL(raw []byte) StreamSummary {
	var summary StreamSummary
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		_ = summary.AddLine(line)
	}
	return summary
}

func normalizeEvent(object map[string]any, sequence int) StreamEvent {
	eventType := firstString(object, "type", "event", "kind")
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	sessionID := firstString(object, "session_id", "sessionId", "chat_id", "chatId")
	text, source := finalText(object, eventType)
	tool := isToolEvent(object, eventType)
	terminal, status := terminalEvidence(object, eventType)
	return StreamEvent{Sequence: sequence, Type: eventType, SessionID: sessionID, Text: text, TextSource: source, Tool: tool, Terminal: terminal, TerminalStatus: status}
}

func finalText(object map[string]any, eventType string) (string, string) {
	if eventType == "result" || eventType == "response.completed" || eventType == "message.completed" || eventType == "assistant_message" || eventType == "response.output_text.done" {
		if value, ok := object["result"].(string); ok && strings.TrimSpace(value) != "" && !truthy(object["is_error"]) {
			return value, eventType + ".result"
		}
		if value, ok := object["text"].(string); ok && strings.TrimSpace(value) != "" {
			return value, eventType + ".text"
		}
	}
	if eventType == "assistant" || eventType == "message" {
		if value, ok := object["text"].(string); ok && strings.TrimSpace(value) != "" {
			return value, eventType + ".text"
		}
		if message, ok := object["message"].(map[string]any); ok {
			if value, source := textFromContent(message["content"]); value != "" {
				return value, eventType + "." + source
			}
		}
		if value, source := textFromContent(object["content"]); value != "" {
			return value, eventType + "." + source
		}
	}
	if eventType == "item.completed" {
		if item, ok := object["item"].(map[string]any); ok {
			if strings.EqualFold(firstString(item, "type", "kind"), "agent_message") {
				if value, ok := item["text"].(string); ok {
					return value, eventType + ".agent_message"
				}
			}
		}
	}
	return "", ""
}

func textFromContent(value any) (string, string) {
	parts, ok := value.([]any)
	if !ok {
		return "", ""
	}
	var texts []string
	for _, part := range parts {
		object, ok := part.(map[string]any)
		if !ok {
			continue
		}
		kind := strings.ToLower(firstString(object, "type", "kind"))
		if kind != "text" && kind != "output_text" && kind != "text_delta" {
			continue
		}
		if text, ok := object["text"].(string); ok && strings.TrimSpace(text) != "" {
			texts = append(texts, text)
		}
	}
	return strings.TrimSpace(strings.Join(texts, "")), "content"
}

func isToolEvent(object map[string]any, eventType string) bool {
	if strings.Contains(eventType, "tool") || strings.Contains(eventType, "function") {
		return true
	}
	if name := firstString(object, "tool_name", "toolName"); name != "" {
		return true
	}
	if message, ok := object["message"].(map[string]any); ok {
		if _, source := nonTextContent(message["content"]); source != "" {
			return true
		}
	}
	return false
}

func nonTextContent(value any) (bool, string) {
	parts, ok := value.([]any)
	if !ok {
		return false, ""
	}
	for _, part := range parts {
		object, ok := part.(map[string]any)
		if !ok {
			continue
		}
		kind := strings.ToLower(firstString(object, "type", "kind"))
		if kind == "tool_use" || kind == "tool_call" || kind == "function_call" {
			return true, kind
		}
	}
	return false, ""
}

func terminalEvidence(object map[string]any, eventType string) (bool, string) {
	terminal := eventType == "result" || eventType == "turn_ended" || eventType == "turn.completed" || eventType == "response.completed" || eventType == "session.ended" || eventType == "session_end"
	if value, ok := object["terminal"].(bool); ok {
		terminal = terminal || value
	}
	status := strings.ToLower(firstString(object, "status", "subtype", "stop_reason"))
	return terminal, status
}

func firstString(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func truthy(value any) bool {
	result, ok := value.(bool)
	return ok && result
}
