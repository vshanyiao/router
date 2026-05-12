package anthropic

import (
	"encoding/json"

	"github.com/admin/maas-router/proxy/internal/canonical"
)

// FrameEvent represents one named-event SSE frame to write to the wire.
type FrameEvent struct {
	Event string
	Data  string
}

// StreamMapper maintains state across a single Anthropic-format stream so
// content_block_start / _delta / _stop events stay paired correctly.
type StreamMapper struct {
	ID    string
	Model string

	startSent        bool
	currentBlockIdx  int
	currentBlockOpen bool
	currentBlockKind string
}

func NewStreamMapper(id, modelAlias string) *StreamMapper {
	return &StreamMapper{ID: id, Model: modelAlias, currentBlockIdx: -1}
}

func (m *StreamMapper) Start(promptTokens int) []FrameEvent {
	if m.startSent {
		return nil
	}
	m.startSent = true
	payload := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": m.ID, "type": "message", "role": "assistant", "model": m.Model,
			"content": []any{}, "usage": map[string]any{"input_tokens": promptTokens, "output_tokens": 0},
		},
	}
	b, _ := json.Marshal(payload)
	return []FrameEvent{{Event: "message_start", Data: string(b)}}
}

// Map produces 0..N frame events for a canonical event.
func (m *StreamMapper) Map(evt canonical.StreamEvent) []FrameEvent {
	var out []FrameEvent
	switch evt.Type {
	case canonical.StreamEventContent:
		if evt.ContentDelta == "" {
			return nil
		}
		out = append(out, m.ensureBlock("text")...)
		payload := map[string]any{
			"type":  "content_block_delta",
			"index": m.currentBlockIdx,
			"delta": map[string]any{"type": "text_delta", "text": evt.ContentDelta},
		}
		b, _ := json.Marshal(payload)
		out = append(out, FrameEvent{Event: "content_block_delta", Data: string(b)})
	case canonical.StreamEventToolCallStart:
		out = append(out, m.closeBlock()...)
		m.currentBlockIdx++
		m.currentBlockKind = "tool_use"
		m.currentBlockOpen = true
		payload := map[string]any{
			"type":  "content_block_start",
			"index": m.currentBlockIdx,
			"content_block": map[string]any{
				"type": "tool_use", "id": evt.ToolCallID, "name": evt.ToolCallName, "input": map[string]any{},
			},
		}
		b, _ := json.Marshal(payload)
		out = append(out, FrameEvent{Event: "content_block_start", Data: string(b)})
	case canonical.StreamEventToolCallDelta:
		payload := map[string]any{
			"type":  "content_block_delta",
			"index": m.currentBlockIdx,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": evt.ToolCallArgsDelta},
		}
		b, _ := json.Marshal(payload)
		out = append(out, FrameEvent{Event: "content_block_delta", Data: string(b)})
	case canonical.StreamEventStop:
		out = append(out, m.closeBlock()...)
		payload := map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": mapStopReason(evt.StopReason)},
			"usage": map[string]any{"output_tokens": 0},
		}
		if evt.Usage != nil {
			payload["usage"] = map[string]any{"output_tokens": evt.Usage.CompletionTokens}
		}
		b, _ := json.Marshal(payload)
		out = append(out, FrameEvent{Event: "message_delta", Data: string(b)})
		out = append(out, FrameEvent{Event: "message_stop", Data: `{"type":"message_stop"}`})
	}
	return out
}

func (m *StreamMapper) ensureBlock(kind string) []FrameEvent {
	if m.currentBlockOpen && m.currentBlockKind == kind {
		return nil
	}
	out := m.closeBlock()
	m.currentBlockIdx++
	m.currentBlockKind = kind
	m.currentBlockOpen = true
	payload := map[string]any{
		"type":  "content_block_start",
		"index": m.currentBlockIdx,
		"content_block": map[string]any{"type": kind, "text": ""},
	}
	b, _ := json.Marshal(payload)
	return append(out, FrameEvent{Event: "content_block_start", Data: string(b)})
}

func (m *StreamMapper) closeBlock() []FrameEvent {
	if !m.currentBlockOpen {
		return nil
	}
	m.currentBlockOpen = false
	payload := map[string]any{"type": "content_block_stop", "index": m.currentBlockIdx}
	b, _ := json.Marshal(payload)
	return []FrameEvent{{Event: "content_block_stop", Data: string(b)}}
}
