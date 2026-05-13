package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/admin/maas-router/proxy/internal/canonical"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamMapper_StartEmitsMessageStart(t *testing.T) {
	m := NewStreamMapper("msg_1", "claude-haiku-4-5")
	frames := m.Start(42)
	require.Len(t, frames, 1)
	assert.Equal(t, "message_start", frames[0].Event)

	var payload struct {
		Type    string `json:"type"`
		Message struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Usage struct {
				InputTokens int `json:"input_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	require.NoError(t, json.Unmarshal([]byte(frames[0].Data), &payload))
	assert.Equal(t, "message_start", payload.Type)
	assert.Equal(t, "msg_1", payload.Message.ID)
	assert.Equal(t, "claude-haiku-4-5", payload.Message.Model)
	assert.Equal(t, 42, payload.Message.Usage.InputTokens)
}

func TestStreamMapper_StartIsIdempotent(t *testing.T) {
	m := NewStreamMapper("msg_1", "m")
	require.Len(t, m.Start(0), 1)
	require.Len(t, m.Start(0), 0, "Start should be idempotent — only first call emits")
}

func TestStreamMapper_ContentDeltaStartsTextBlock(t *testing.T) {
	m := NewStreamMapper("msg_1", "m")
	frames := m.Map(canonical.StreamEvent{Type: canonical.StreamEventContent, ContentDelta: "hi"})
	require.Len(t, frames, 2, "first content delta should emit content_block_start + content_block_delta")
	assert.Equal(t, "content_block_start", frames[0].Event)
	assert.Equal(t, "content_block_delta", frames[1].Event)

	// Verify the start payload has no "text" field (was a wire-format bug)
	var startPayload struct {
		ContentBlock map[string]any `json:"content_block"`
	}
	require.NoError(t, json.Unmarshal([]byte(frames[0].Data), &startPayload))
	_, hasText := startPayload.ContentBlock["text"]
	assert.False(t, hasText, "text content_block_start should not include a 'text' field; text arrives in deltas only")

	// Verify the delta carries the text
	var deltaPayload struct {
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	}
	require.NoError(t, json.Unmarshal([]byte(frames[1].Data), &deltaPayload))
	assert.Equal(t, "text_delta", deltaPayload.Delta.Type)
	assert.Equal(t, "hi", deltaPayload.Delta.Text)
}

func TestStreamMapper_ConsecutiveContentDeltasReuseBlock(t *testing.T) {
	m := NewStreamMapper("msg_1", "m")
	_ = m.Map(canonical.StreamEvent{Type: canonical.StreamEventContent, ContentDelta: "a"})
	frames := m.Map(canonical.StreamEvent{Type: canonical.StreamEventContent, ContentDelta: "b"})
	require.Len(t, frames, 1, "second content delta to same text block should NOT emit a new start")
	assert.Equal(t, "content_block_delta", frames[0].Event)
}

func TestStreamMapper_ToolCallStartClosesPriorTextBlock(t *testing.T) {
	m := NewStreamMapper("msg_1", "m")
	_ = m.Map(canonical.StreamEvent{Type: canonical.StreamEventContent, ContentDelta: "thinking..."})
	frames := m.Map(canonical.StreamEvent{
		Type:         canonical.StreamEventToolCallStart,
		ToolCallID:   "toolu_X",
		ToolCallName: "search",
	})
	require.Len(t, frames, 2, "tool_call_start should emit content_block_stop (close text) + content_block_start (open tool_use)")
	assert.Equal(t, "content_block_stop", frames[0].Event)
	assert.Equal(t, "content_block_start", frames[1].Event)

	var payload struct {
		ContentBlock struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"content_block"`
	}
	require.NoError(t, json.Unmarshal([]byte(frames[1].Data), &payload))
	assert.Equal(t, "tool_use", payload.ContentBlock.Type)
	assert.Equal(t, "toolu_X", payload.ContentBlock.ID)
	assert.Equal(t, "search", payload.ContentBlock.Name)
}

func TestStreamMapper_ToolCallDeltaEmitsInputJsonDelta(t *testing.T) {
	m := NewStreamMapper("msg_1", "m")
	_ = m.Map(canonical.StreamEvent{Type: canonical.StreamEventToolCallStart, ToolCallID: "toolu_X", ToolCallName: "f"})
	frames := m.Map(canonical.StreamEvent{
		Type:              canonical.StreamEventToolCallDelta,
		ToolCallID:        "toolu_X",
		ToolCallArgsDelta: `{"x":1}`,
	})
	require.Len(t, frames, 1)
	assert.Equal(t, "content_block_delta", frames[0].Event)

	var payload struct {
		Delta struct {
			Type        string `json:"type"`
			PartialJSON string `json:"partial_json"`
		} `json:"delta"`
	}
	require.NoError(t, json.Unmarshal([]byte(frames[0].Data), &payload))
	assert.Equal(t, "input_json_delta", payload.Delta.Type)
	assert.Equal(t, `{"x":1}`, payload.Delta.PartialJSON)
}

func TestStreamMapper_ToolCallStopEmitsContentBlockStop(t *testing.T) {
	m := NewStreamMapper("msg_1", "m")
	_ = m.Map(canonical.StreamEvent{Type: canonical.StreamEventToolCallStart, ToolCallID: "toolu_X", ToolCallName: "f"})
	frames := m.Map(canonical.StreamEvent{Type: canonical.StreamEventToolCallStop, ToolCallID: "toolu_X"})
	require.Len(t, frames, 1, "ToolCallStop must emit a matching content_block_stop frame")
	assert.Equal(t, "content_block_stop", frames[0].Event)
}

func TestStreamMapper_StopClosesBlockAndEmitsMessageDelta(t *testing.T) {
	m := NewStreamMapper("msg_1", "m")
	_ = m.Map(canonical.StreamEvent{Type: canonical.StreamEventContent, ContentDelta: "done"})
	frames := m.Map(canonical.StreamEvent{
		Type:       canonical.StreamEventStop,
		StopReason: "stop",
		Usage:      &canonical.Usage{PromptTokens: 10, CompletionTokens: 5},
	})
	// Expect: content_block_stop (close text block), message_delta, message_stop
	require.Len(t, frames, 3)
	assert.Equal(t, "content_block_stop", frames[0].Event)
	assert.Equal(t, "message_delta", frames[1].Event)
	assert.Equal(t, "message_stop", frames[2].Event)

	var deltaPayload struct {
		Delta struct {
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
		Usage struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	require.NoError(t, json.Unmarshal([]byte(frames[1].Data), &deltaPayload))
	assert.Equal(t, "end_turn", deltaPayload.Delta.StopReason)
	assert.Equal(t, 5, deltaPayload.Usage.OutputTokens)
}

func TestStreamMapper_StopMapsCancelled(t *testing.T) {
	m := NewStreamMapper("msg_1", "m")
	frames := m.Map(canonical.StreamEvent{Type: canonical.StreamEventStop, StopReason: "cancelled"})
	// Find the message_delta frame and verify cancelled → end_turn (not stop_sequence)
	for _, f := range frames {
		if f.Event != "message_delta" {
			continue
		}
		var payload struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
		}
		require.NoError(t, json.Unmarshal([]byte(f.Data), &payload))
		assert.Equal(t, "end_turn", payload.Delta.StopReason, "cancelled should map to end_turn, not stop_sequence")
		return
	}
	t.Fatal("no message_delta frame emitted")
}
