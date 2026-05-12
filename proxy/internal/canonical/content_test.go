package canonical

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContentBlock_Text(t *testing.T) {
	b := TextBlock("hello")
	assert.Equal(t, BlockText, b.Type)
	assert.Equal(t, "hello", b.Text)
}

func TestContentBlock_Image(t *testing.T) {
	b := ImageBlock("https://example.com/foo.png", "")
	assert.Equal(t, BlockImage, b.Type)
	assert.Equal(t, "https://example.com/foo.png", b.ImageURL)
	assert.Empty(t, b.ImageData)
}

func TestContentBlock_ToolUse(t *testing.T) {
	b := ToolUseBlock("call_abc", "get_weather", []byte(`{"city":"Paris"}`))
	assert.Equal(t, BlockToolUse, b.Type)
	assert.Equal(t, "call_abc", b.ToolUseID)
	assert.Equal(t, "get_weather", b.ToolName)
	assert.JSONEq(t, `{"city":"Paris"}`, string(b.ToolInput))
}

func TestContentBlock_ToolResult(t *testing.T) {
	b := ToolResultBlock("call_abc", "Sunny, 22°C")
	assert.Equal(t, BlockToolResult, b.Type)
	assert.Equal(t, "call_abc", b.ToolUseID)
	assert.Equal(t, "Sunny, 22°C", b.ToolResult)
}
