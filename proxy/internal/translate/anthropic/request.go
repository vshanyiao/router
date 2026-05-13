// Package anthropic handles translation between Anthropic's /v1/messages
// surface wire format and the canonical IR.
package anthropic

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/admin/maas-router/proxy/internal/canonical"
)

type MessagesRequest struct {
	Model         string          `json:"model"`
	System        json.RawMessage `json:"system,omitempty"`
	Messages      []WireMessage   `json:"messages"`
	MaxTokens     int             `json:"max_tokens"`
	Temperature   *float64        `json:"temperature,omitempty"`
	TopP          *float64        `json:"top_p,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	Tools         []ToolWire      `json:"tools,omitempty"`
	ToolChoice    json.RawMessage `json:"tool_choice,omitempty"`
}

type WireMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type ContentBlockWire struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Source    *ImageSource    `json:"source,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type ImageSource struct {
	Type      string `json:"type"`
	URL       string `json:"url,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

type ToolWire struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ToCanonical converts an Anthropic /v1/messages request into canonical IR.
func ToCanonical(req MessagesRequest) (canonical.Request, error) {
	out := canonical.Request{
		Model:         req.Model,
		MaxTokens:     req.MaxTokens,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		StopSequences: req.StopSequences,
		Stream:        req.Stream,
	}

	if len(req.System) > 0 {
		var s string
		if err := json.Unmarshal(req.System, &s); err == nil {
			out.System = s
		} else {
			var blocks []ContentBlockWire
			if err := json.Unmarshal(req.System, &blocks); err != nil {
				return canonical.Request{}, fmt.Errorf("system must be string or array: %w", err)
			}
			for _, b := range blocks {
				if b.Type == "text" {
					out.System += b.Text
				}
			}
		}
	}

	for _, t := range req.Tools {
		out.Tools = append(out.Tools, canonical.Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}

	out.ToolChoice = parseToolChoice(req.ToolChoice)

	for i, m := range req.Messages {
		blocks, err := parseContent(m.Content)
		if err != nil {
			return canonical.Request{}, fmt.Errorf("messages[%d]: %w", i, err)
		}
		role := canonical.RoleUser
		if m.Role == "assistant" {
			role = canonical.RoleAssistant
		}
		out.Messages = append(out.Messages, canonical.Message{Role: role, Content: blocks})
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
	var parts []ContentBlockWire
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("content must be string or array of blocks: %w", err)
	}
	out := make([]canonical.ContentBlock, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case "text":
			out = append(out, canonical.TextBlock(p.Text))
		case "image":
			if p.Source == nil {
				return nil, fmt.Errorf("image block missing source")
			}
			if p.Source.Type == "url" {
				out = append(out, canonical.ImageBlock(p.Source.URL, p.Source.MediaType))
			} else if p.Source.Type == "base64" {
				// Anthropic delivers image bytes pre-encoded as base64 in source.data.
				// Decode to raw bytes here so downstream providers can re-encode as
				// they need (OAI inlines as data: URL; Gemini wants base64 again;
				// Anthropic also wants base64). Storing the raw base64 string as
				// []byte would cause double-encoding at every provider.
				data, err := base64.StdEncoding.DecodeString(p.Source.Data)
				if err != nil {
					return nil, fmt.Errorf("image source.data invalid base64: %w", err)
				}
				out = append(out, canonical.ImageDataBlock(data, p.Source.MediaType))
			}
		case "tool_use":
			out = append(out, canonical.ToolUseBlock(p.ID, p.Name, p.Input))
		case "tool_result":
			var resText string
			if err := json.Unmarshal(p.Content, &resText); err != nil {
				var sub []ContentBlockWire
				if err := json.Unmarshal(p.Content, &sub); err == nil {
					for _, s := range sub {
						if s.Type == "text" {
							resText += s.Text
						}
					}
				}
			}
			out = append(out, canonical.ToolResultBlock(p.ToolUseID, resText))
		default:
			return nil, fmt.Errorf("unsupported content block type %q", p.Type)
		}
	}
	return out, nil
}

func parseToolChoice(raw json.RawMessage) canonical.ToolChoice {
	if len(raw) == 0 {
		return canonical.ToolChoice{Mode: canonical.ToolChoiceAuto}
	}
	var obj struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		switch obj.Type {
		case "any":
			return canonical.ToolChoice{Mode: canonical.ToolChoiceAny}
		case "tool":
			return canonical.ToolChoice{Mode: canonical.ToolChoiceSpecific, ToolName: obj.Name}
		case "auto":
			return canonical.ToolChoice{Mode: canonical.ToolChoiceAuto}
		}
	}
	return canonical.ToolChoice{Mode: canonical.ToolChoiceAuto}
}
