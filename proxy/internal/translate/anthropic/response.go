package anthropic

import "github.com/admin/maas-router/proxy/internal/canonical"

// MessagesResponse mirrors Anthropic's /v1/messages response.
type MessagesResponse struct {
	ID         string             `json:"id"`
	Type       string             `json:"type"`
	Role       string             `json:"role"`
	Model      string             `json:"model"`
	Content    []ContentBlockWire `json:"content"`
	StopReason string             `json:"stop_reason"`
	Usage      UsageWire          `json:"usage"`
}

type UsageWire struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func FromCanonical(id, modelAlias string, r canonical.Response) MessagesResponse {
	content := make([]ContentBlockWire, 0, len(r.Content))
	for _, b := range r.Content {
		switch b.Type {
		case canonical.BlockText:
			content = append(content, ContentBlockWire{Type: "text", Text: b.Text})
		case canonical.BlockToolUse:
			content = append(content, ContentBlockWire{
				Type: "tool_use", ID: b.ToolUseID, Name: b.ToolName, Input: b.ToolInput,
			})
		}
	}
	return MessagesResponse{
		ID:         id,
		Type:       "message",
		Role:       "assistant",
		Model:      modelAlias,
		Content:    content,
		StopReason: mapStopReason(r.StopReason),
		Usage:      UsageWire{InputTokens: r.Usage.PromptTokens, OutputTokens: r.Usage.CompletionTokens},
	}
}

func mapStopReason(r string) string {
	switch r {
	case "tool_use":
		return "tool_use"
	case "length":
		return "max_tokens"
	case "cancelled":
		// Anthropic has no native concept of "client cancelled mid-stream".
		// "stop_sequence" would imply a stop string was triggered (wrong);
		// "end_turn" is the least misleading neutral signal.
		return "end_turn"
	default:
		return "end_turn"
	}
}
