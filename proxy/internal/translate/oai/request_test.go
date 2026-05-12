package oai

import (
	"encoding/json"
	"testing"

	"github.com/admin/maas-router/proxy/internal/canonical"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToCanonical_StringContent(t *testing.T) {
	req := ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	}
	got, err := ToCanonical(req)
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	require.Len(t, got.Messages[0].Content, 1)
	assert.Equal(t, canonical.BlockText, got.Messages[0].Content[0].Type)
	assert.Equal(t, "hello", got.Messages[0].Content[0].Text)
}

func TestToCanonical_SystemExtracted(t *testing.T) {
	req := ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "system", Content: json.RawMessage(`"be helpful"`)},
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
	}
	got, err := ToCanonical(req)
	require.NoError(t, err)
	assert.Equal(t, "be helpful", got.System)
	require.Len(t, got.Messages, 1, "system should be extracted, not in messages")
}

func TestToCanonical_ImageContent(t *testing.T) {
	req := ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`[
				{"type":"text","text":"what's in this?"},
				{"type":"image_url","image_url":{"url":"https://x.com/cat.png"}}
			]`)},
		},
	}
	got, err := ToCanonical(req)
	require.NoError(t, err)
	require.Len(t, got.Messages[0].Content, 2)
	assert.Equal(t, canonical.BlockText, got.Messages[0].Content[0].Type)
	assert.Equal(t, canonical.BlockImage, got.Messages[0].Content[1].Type)
	assert.Equal(t, "https://x.com/cat.png", got.Messages[0].Content[1].ImageURL)
}

func TestToCanonical_ToolCallsAndResponses(t *testing.T) {
	req := ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"weather in Paris?"`)},
			{
				Role:    "assistant",
				Content: json.RawMessage(`null`),
				ToolCalls: []ChatToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "get_weather", Arguments: `{"city":"Paris"}`},
				}},
			},
			{Role: "tool", ToolCallID: "call_1", Content: json.RawMessage(`"sunny"`)},
		},
	}
	got, err := ToCanonical(req)
	require.NoError(t, err)
	require.Len(t, got.Messages, 3)
	assert.Equal(t, canonical.RoleAssistant, got.Messages[1].Role)
	require.Len(t, got.Messages[1].Content, 1)
	assert.Equal(t, canonical.BlockToolUse, got.Messages[1].Content[0].Type)
	assert.Equal(t, "call_1", got.Messages[1].Content[0].ToolUseID)
	assert.Equal(t, "get_weather", got.Messages[1].Content[0].ToolName)
	assert.Equal(t, canonical.RoleUser, got.Messages[2].Role)
	require.Len(t, got.Messages[2].Content, 1)
	assert.Equal(t, canonical.BlockToolResult, got.Messages[2].Content[0].Type)
	assert.Equal(t, "call_1", got.Messages[2].Content[0].ToolUseID)
	assert.Equal(t, "sunny", got.Messages[2].Content[0].ToolResult)
}

func TestToCanonical_ToolDefs(t *testing.T) {
	req := ChatCompletionsRequest{
		Model: "gpt-4o",
		Tools: []ToolDef{{
			Type: "function",
			Function: struct {
				Name        string          `json:"name"`
				Description string          `json:"description,omitempty"`
				Parameters  json.RawMessage `json:"parameters"`
			}{
				Name:        "get_weather",
				Description: "get current weather",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			},
		}},
	}
	got, err := ToCanonical(req)
	require.NoError(t, err)
	require.Len(t, got.Tools, 1)
	assert.Equal(t, "get_weather", got.Tools[0].Name)
	assert.JSONEq(t, `{"type":"object"}`, string(got.Tools[0].InputSchema))
}
