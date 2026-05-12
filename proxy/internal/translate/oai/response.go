package oai

import "github.com/admin/maas-router/proxy/internal/canonical"

// ChatCompletionsResponse mirrors OpenAI's response wire format.
type ChatCompletionsResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []Choice       `json:"choices"`
	Usage   UsagePayload   `json:"usage"`
}

type Choice struct {
	Index        int           `json:"index"`
	Message      RespMessage   `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type RespMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	ToolCalls []ChatToolCall `json:"tool_calls,omitempty"`
}

type UsagePayload struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// FromCanonical builds an OAI response from a canonical Response. The
// caller supplies id, model alias, and created timestamp.
func FromCanonical(id, modelAlias string, created int64, r canonical.Response) ChatCompletionsResponse {
	var text string
	var toolCalls []ChatToolCall
	for _, b := range r.Content {
		switch b.Type {
		case canonical.BlockText:
			text += b.Text
		case canonical.BlockToolUse:
			tc := ChatToolCall{ID: b.ToolUseID, Type: "function"}
			tc.Function.Name = b.ToolName
			tc.Function.Arguments = string(b.ToolInput)
			toolCalls = append(toolCalls, tc)
		}
	}
	finish := mapStopReason(r.StopReason)
	return ChatCompletionsResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   modelAlias,
		Choices: []Choice{{
			Index:        0,
			Message:      RespMessage{Role: "assistant", Content: text, ToolCalls: toolCalls},
			FinishReason: finish,
		}},
		Usage: UsagePayload{
			PromptTokens:     r.Usage.PromptTokens,
			CompletionTokens: r.Usage.CompletionTokens,
			TotalTokens:      r.Usage.PromptTokens + r.Usage.CompletionTokens,
		},
	}
}

func mapStopReason(r string) string {
	switch r {
	case "tool_use":
		return "tool_calls"
	case "length":
		return "length"
	case "cancelled":
		return "cancelled"
	default:
		return "stop"
	}
}
