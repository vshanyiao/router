package canonical

import "encoding/json"

// Tool is a function the model can call. Schema follows JSON Schema and is
// passed through unchanged to providers.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ToolChoiceMode controls whether/which tool the model should call.
type ToolChoiceMode string

const (
	ToolChoiceAuto     ToolChoiceMode = "auto"     // model decides (default)
	ToolChoiceNone     ToolChoiceMode = "none"     // model must respond directly
	ToolChoiceAny      ToolChoiceMode = "any"      // model must call some tool
	ToolChoiceSpecific ToolChoiceMode = "specific" // model must call ToolName
)

type ToolChoice struct {
	Mode     ToolChoiceMode
	ToolName string // only when Mode == ToolChoiceSpecific
}
