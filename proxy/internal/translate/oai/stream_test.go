package oai

import (
	"encoding/json"
	"testing"

	"github.com/admin/maas-router/proxy/internal/canonical"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamMapper_ContentDelta(t *testing.T) {
	m := NewStreamMapper("chatcmpl-test", "openai/gpt-4o", 1700000000)
	chunk := m.Map(canonical.StreamEvent{
		Type:         canonical.StreamEventContent,
		ContentDelta: "hello",
	})
	require.NotNil(t, chunk)
	assert.Equal(t, "chatcmpl-test", chunk.ID)
	assert.Equal(t, "chat.completion.chunk", chunk.Object)
	assert.Equal(t, "openai/gpt-4o", chunk.Model)
	require.Len(t, chunk.Choices, 1)
	assert.Equal(t, "hello", chunk.Choices[0].Delta.Content)
	assert.Nil(t, chunk.Choices[0].FinishReason)
}

func TestStreamMapper_EmptyContentDeltaIgnored(t *testing.T) {
	m := NewStreamMapper("id", "m", 0)
	chunk := m.Map(canonical.StreamEvent{Type: canonical.StreamEventContent, ContentDelta: ""})
	assert.Nil(t, chunk, "empty content delta should not produce a chunk")
}

func TestStreamMapper_ToolCallStartAssignsIndexZero(t *testing.T) {
	m := NewStreamMapper("id", "m", 0)
	chunk := m.Map(canonical.StreamEvent{
		Type:         canonical.StreamEventToolCallStart,
		ToolCallID:   "call_abc",
		ToolCallName: "get_weather",
	})
	require.NotNil(t, chunk)
	require.Len(t, chunk.Choices[0].Delta.ToolCalls, 1)
	tc := chunk.Choices[0].Delta.ToolCalls[0]
	assert.Equal(t, 0, tc.Index)
	assert.Equal(t, "call_abc", tc.ID)
	assert.Equal(t, "function", tc.Type)
	assert.Equal(t, "get_weather", tc.Function.Name)
}

func TestStreamMapper_ToolCallDeltaLooksUpIndex(t *testing.T) {
	m := NewStreamMapper("id", "m", 0)
	// Start first tool — gets index 0
	_ = m.Map(canonical.StreamEvent{Type: canonical.StreamEventToolCallStart, ToolCallID: "call_1", ToolCallName: "a"})
	// Start second tool — gets index 1
	_ = m.Map(canonical.StreamEvent{Type: canonical.StreamEventToolCallStart, ToolCallID: "call_2", ToolCallName: "b"})

	// Delta for the SECOND tool — should have index 1
	chunk := m.Map(canonical.StreamEvent{
		Type:              canonical.StreamEventToolCallDelta,
		ToolCallID:        "call_2",
		ToolCallArgsDelta: `{"x":1}`,
	})
	require.NotNil(t, chunk)
	require.Len(t, chunk.Choices[0].Delta.ToolCalls, 1)
	tc := chunk.Choices[0].Delta.ToolCalls[0]
	assert.Equal(t, 1, tc.Index)
	assert.Equal(t, `{"x":1}`, tc.Function.Arguments)
}

func TestStreamMapper_ToolCallDeltaUnknownIDIsDropped(t *testing.T) {
	m := NewStreamMapper("id", "m", 0)
	chunk := m.Map(canonical.StreamEvent{
		Type:              canonical.StreamEventToolCallDelta,
		ToolCallID:        "call_never_started",
		ToolCallArgsDelta: "junk",
	})
	assert.Nil(t, chunk, "delta for unknown tool_call_id should be dropped, not emit malformed chunk")
}

func TestStreamMapper_StopMapsFinishReason(t *testing.T) {
	m := NewStreamMapper("id", "m", 0)
	chunk := m.Map(canonical.StreamEvent{Type: canonical.StreamEventStop, StopReason: "tool_use"})
	require.NotNil(t, chunk)
	require.NotNil(t, chunk.Choices[0].FinishReason)
	assert.Equal(t, "tool_calls", *chunk.Choices[0].FinishReason, "canonical 'tool_use' should map to OAI 'tool_calls'")
}

func TestStreamMapper_StopDefaultsToStop(t *testing.T) {
	m := NewStreamMapper("id", "m", 0)
	chunk := m.Map(canonical.StreamEvent{Type: canonical.StreamEventStop, StopReason: ""})
	require.NotNil(t, chunk)
	require.NotNil(t, chunk.Choices[0].FinishReason)
	assert.Equal(t, "stop", *chunk.Choices[0].FinishReason)
}

func TestStreamMapper_FullSequenceIsValidJSON(t *testing.T) {
	// Verify each emitted chunk marshals to valid JSON with the expected fields.
	m := NewStreamMapper("chatcmpl-1", "openai/gpt-4o", 1700000000)
	events := []canonical.StreamEvent{
		{Type: canonical.StreamEventContent, ContentDelta: "Hello"},
		{Type: canonical.StreamEventContent, ContentDelta: ", world"},
		{Type: canonical.StreamEventStop, StopReason: "stop"},
	}
	for _, evt := range events {
		chunk := m.Map(evt)
		if chunk == nil {
			continue
		}
		b, err := json.Marshal(chunk)
		require.NoError(t, err)
		// All chunks should declare object as "chat.completion.chunk"
		assert.Contains(t, string(b), `"object":"chat.completion.chunk"`)
	}
}
