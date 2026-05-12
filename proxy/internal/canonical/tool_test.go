package canonical

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTool_DefaultToolChoice(t *testing.T) {
	tc := ToolChoice{}
	assert.Empty(t, string(tc.Mode), "zero-value mode is empty; handlers should treat as auto")
}

func TestToolChoice_Specific(t *testing.T) {
	tc := ToolChoice{Mode: ToolChoiceSpecific, ToolName: "get_weather"}
	assert.Equal(t, "get_weather", tc.ToolName)
}
