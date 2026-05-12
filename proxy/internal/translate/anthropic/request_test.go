package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/admin/maas-router/proxy/internal/canonical"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToCanonical_System_Plain(t *testing.T) {
	req := MessagesRequest{
		Model:  "claude-sonnet-4-6",
		System: json.RawMessage(`"be helpful"`),
		Messages: []WireMessage{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
		MaxTokens: 100,
	}
	got, err := ToCanonical(req)
	require.NoError(t, err)
	assert.Equal(t, "be helpful", got.System)
}

func TestToCanonical_ImageAndToolUse(t *testing.T) {
	req := MessagesRequest{
		Model: "claude-sonnet-4-6",
		Messages: []WireMessage{
			{Role: "user", Content: json.RawMessage(`[
				{"type":"text","text":"what's this?"},
				{"type":"image","source":{"type":"url","url":"https://x.com/cat.png","media_type":"image/png"}}
			]`)},
			{Role: "assistant", Content: json.RawMessage(`[
				{"type":"text","text":"checking..."},
				{"type":"tool_use","id":"toolu_1","name":"vision","input":{"region":"main"}}
			]`)},
		},
		MaxTokens: 100,
	}
	got, err := ToCanonical(req)
	require.NoError(t, err)
	require.Len(t, got.Messages, 2)
	require.Len(t, got.Messages[0].Content, 2)
	assert.Equal(t, canonical.BlockImage, got.Messages[0].Content[1].Type)
	assert.Equal(t, "https://x.com/cat.png", got.Messages[0].Content[1].ImageURL)
	require.Len(t, got.Messages[1].Content, 2)
	assert.Equal(t, canonical.BlockToolUse, got.Messages[1].Content[1].Type)
	assert.Equal(t, "toolu_1", got.Messages[1].Content[1].ToolUseID)
	assert.Equal(t, "vision", got.Messages[1].Content[1].ToolName)
}

func TestToCanonical_ToolResult(t *testing.T) {
	req := MessagesRequest{
		Model: "claude-sonnet-4-6",
		Messages: []WireMessage{
			{Role: "user", Content: json.RawMessage(`[
				{"type":"tool_result","tool_use_id":"toolu_1","content":"cat detected"}
			]`)},
		},
		MaxTokens: 100,
	}
	got, err := ToCanonical(req)
	require.NoError(t, err)
	require.Len(t, got.Messages[0].Content, 1)
	assert.Equal(t, canonical.BlockToolResult, got.Messages[0].Content[0].Type)
	assert.Equal(t, "toolu_1", got.Messages[0].Content[0].ToolUseID)
	assert.Equal(t, "cat detected", got.Messages[0].Content[0].ToolResult)
}
