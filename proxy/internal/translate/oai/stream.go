package oai

import "github.com/admin/maas-router/proxy/internal/canonical"

// StreamingChunk is one SSE event in OAI's stream wire format.
type StreamingChunk struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []StreamingChoice  `json:"choices"`
	Usage   *UsagePayload      `json:"usage,omitempty"`
}

type StreamingChoice struct {
	Index        int                `json:"index"`
	Delta        StreamingDelta     `json:"delta"`
	FinishReason *string            `json:"finish_reason,omitempty"`
}

type StreamingDelta struct {
	Role      string                 `json:"role,omitempty"`
	Content   string                 `json:"content,omitempty"`
	ToolCalls []StreamingToolCall    `json:"tool_calls,omitempty"`
}

type StreamingToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function,omitempty"`
}

// StreamMapper translates canonical stream events into OAI SSE chunks. It is
// stateful because tool_call streams need to remember the index they were
// assigned (OAI numbers tool calls within a single response).
type StreamMapper struct {
	ID         string
	Model      string
	Created    int64
	toolIndex  map[string]int // canonical ToolCallID → OAI tool_calls index
	nextIndex  int
}

func NewStreamMapper(id, modelAlias string, created int64) *StreamMapper {
	return &StreamMapper{ID: id, Model: modelAlias, Created: created, toolIndex: map[string]int{}}
}

// Map produces zero or one StreamingChunk for a canonical event.
// Returns nil for events that don't translate to a chunk.
func (m *StreamMapper) Map(evt canonical.StreamEvent) *StreamingChunk {
	switch evt.Type {
	case canonical.StreamEventContent:
		if evt.ContentDelta == "" {
			return nil
		}
		return &StreamingChunk{
			ID: m.ID, Object: "chat.completion.chunk", Created: m.Created, Model: m.Model,
			Choices: []StreamingChoice{{Index: 0, Delta: StreamingDelta{Content: evt.ContentDelta}}},
		}
	case canonical.StreamEventToolCallStart:
		idx := m.nextIndex
		m.toolIndex[evt.ToolCallID] = idx
		m.nextIndex++
		tc := StreamingToolCall{Index: idx, ID: evt.ToolCallID, Type: "function"}
		tc.Function.Name = evt.ToolCallName
		return &StreamingChunk{
			ID: m.ID, Object: "chat.completion.chunk", Created: m.Created, Model: m.Model,
			Choices: []StreamingChoice{{Index: 0, Delta: StreamingDelta{ToolCalls: []StreamingToolCall{tc}}}},
		}
	case canonical.StreamEventToolCallDelta:
		idx, ok := m.toolIndex[evt.ToolCallID]
		if !ok {
			return nil
		}
		tc := StreamingToolCall{Index: idx}
		tc.Function.Arguments = evt.ToolCallArgsDelta
		return &StreamingChunk{
			ID: m.ID, Object: "chat.completion.chunk", Created: m.Created, Model: m.Model,
			Choices: []StreamingChoice{{Index: 0, Delta: StreamingDelta{ToolCalls: []StreamingToolCall{tc}}}},
		}
	case canonical.StreamEventStop:
		fr := mapStopReason(evt.StopReason)
		return &StreamingChunk{
			ID: m.ID, Object: "chat.completion.chunk", Created: m.Created, Model: m.Model,
			Choices: []StreamingChoice{{Index: 0, Delta: StreamingDelta{}, FinishReason: &fr}},
		}
	}
	return nil
}
