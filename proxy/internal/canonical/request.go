package canonical

// Role enumerates message roles. System is separate from Messages (see Request).
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool" // OAI-style tool response messages; both Anthropic
	                            // and Gemini fold these into user messages with
	                            // tool_result content blocks during lowering.
)

// Message is one turn in the conversation. Content is always a slice (a wire-
// format string becomes []ContentBlock{TextBlock(s)} at the surface boundary).
type Message struct {
	Role    Role
	Content []ContentBlock
}

// Request is the canonical inference request. Surface handlers lower into this;
// provider adapters consume it. Note: System is separate so providers that have
// a dedicated system field (Anthropic, Gemini) can populate it directly without
// hunting through Messages.
type Request struct {
	Model         string   // upstream model id (the provider-native name)
	System        string   // empty if not provided
	Messages      []Message

	MaxTokens     int
	Temperature   *float64
	TopP          *float64
	StopSequences []string

	Tools         []Tool
	ToolChoice    ToolChoice

	Stream        bool
}
