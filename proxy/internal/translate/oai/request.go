// Package oai handles translation between the OpenAI-compatible API surface
// (POST /v1/chat/completions wire format) and the canonical IR.
package oai

import (
	"encoding/json"
	"fmt"

	"github.com/admin/maas-router/proxy/internal/canonical"
)

// ChatCompletionsRequest matches OpenAI's wire format. Use json.RawMessage for
// the parts of the protocol we want to forward without parsing.
type ChatCompletionsRequest struct {
	Model       string                     `json:"model"`
	Messages    []ChatMessage              `json:"messages"`
	MaxTokens   int                        `json:"max_tokens,omitempty"`
	Temperature *float64                   `json:"temperature,omitempty"`
	TopP        *float64                   `json:"top_p,omitempty"`
	Stop        StopField                  `json:"stop,omitempty"`
	Stream      bool                       `json:"stream,omitempty"`
	Tools       []ToolDef                  `json:"tools,omitempty"`
	ToolChoice  json.RawMessage            `json:"tool_choice,omitempty"`
}

// StopField is OAI's "stop" — accepts a single string or a list of strings.
type StopField []string

func (s *StopField) UnmarshalJSON(data []byte) error {
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*s = arr
		return nil
	}
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*s = []string{single}
		return nil
	}
	return fmt.Errorf("stop must be a string or array of strings")
}

// ChatMessage's Content is either a string or an array of content parts.
type ChatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  []ChatToolCall  `json:"tool_calls,omitempty"`  // assistant role only
	ToolCallID string          `json:"tool_call_id,omitempty"` // tool role only
}

type ContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *ContentImageURL `json:"image_url,omitempty"`
}

type ContentImageURL struct {
	URL string `json:"url"`
}

type ChatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

// ToCanonical converts an OAI request into the canonical IR.
func ToCanonical(req ChatCompletionsRequest) (canonical.Request, error) {
	out := canonical.Request{
		Model:         req.Model,
		MaxTokens:     req.MaxTokens,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		StopSequences: []string(req.Stop),
		Stream:        req.Stream,
	}

	for _, t := range req.Tools {
		out.Tools = append(out.Tools, canonical.Tool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}

	out.ToolChoice = parseToolChoice(req.ToolChoice)

	for _, m := range req.Messages {
		blocks, err := parseContent(m.Content)
		if err != nil {
			return canonical.Request{}, fmt.Errorf("message[%s] content: %w", m.Role, err)
		}
		switch m.Role {
		case "system":
			if out.System != "" {
				out.System += "\n\n"
			}
			out.System += contentToText(blocks)
		case "tool":
			out.Messages = append(out.Messages, canonical.Message{
				Role: canonical.RoleUser,
				Content: []canonical.ContentBlock{
					canonical.ToolResultBlock(m.ToolCallID, contentToText(blocks)),
				},
			})
		case "assistant":
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, canonical.ToolUseBlock(tc.ID, tc.Function.Name, json.RawMessage(tc.Function.Arguments)))
			}
			out.Messages = append(out.Messages, canonical.Message{
				Role:    canonical.RoleAssistant,
				Content: blocks,
			})
		case "user":
			out.Messages = append(out.Messages, canonical.Message{
				Role:    canonical.RoleUser,
				Content: blocks,
			})
		default:
			return canonical.Request{}, fmt.Errorf("unsupported role %q", m.Role)
		}
	}
	return out, nil
}

func parseContent(raw json.RawMessage) ([]canonical.ContentBlock, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []canonical.ContentBlock{canonical.TextBlock(s)}, nil
	}
	var parts []ContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("content must be string or array: %w", err)
	}
	out := make([]canonical.ContentBlock, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case "text":
			out = append(out, canonical.TextBlock(p.Text))
		case "image_url":
			if p.ImageURL == nil {
				return nil, fmt.Errorf("image_url part missing image_url object")
			}
			out = append(out, canonical.ImageBlock(p.ImageURL.URL, ""))
		default:
			return nil, fmt.Errorf("unsupported content part type %q", p.Type)
		}
	}
	return out, nil
}

func contentToText(blocks []canonical.ContentBlock) string {
	var s string
	for _, b := range blocks {
		if b.Type == canonical.BlockText {
			s += b.Text
		}
	}
	return s
}

func parseToolChoice(raw json.RawMessage) canonical.ToolChoice {
	if len(raw) == 0 {
		return canonical.ToolChoice{Mode: canonical.ToolChoiceAuto}
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch s {
		case "none":
			return canonical.ToolChoice{Mode: canonical.ToolChoiceNone}
		case "required":
			return canonical.ToolChoice{Mode: canonical.ToolChoiceAny}
		default:
			return canonical.ToolChoice{Mode: canonical.ToolChoiceAuto}
		}
	}
	var obj struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Function.Name != "" {
		return canonical.ToolChoice{Mode: canonical.ToolChoiceSpecific, ToolName: obj.Function.Name}
	}
	return canonical.ToolChoice{Mode: canonical.ToolChoiceAuto}
}
