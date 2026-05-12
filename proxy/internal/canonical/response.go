package canonical

// Response is a complete (non-streaming) inference result. The assistant's
// turn is represented as a slice of content blocks (text + zero or more
// tool_use blocks).
type Response struct {
	Content    []ContentBlock
	StopReason string // "stop" | "length" | "tool_use" | "cancelled"
	Usage      Usage
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

// StreamEventType enumerates the events a streaming response can produce.
// Each surface translator maps these to its own wire format.
type StreamEventType string

const (
	StreamEventContent       StreamEventType = "content_delta"
	StreamEventToolCallStart StreamEventType = "tool_call_start"
	StreamEventToolCallDelta StreamEventType = "tool_call_delta"
	StreamEventToolCallStop  StreamEventType = "tool_call_stop"
	StreamEventUsage         StreamEventType = "usage"
	StreamEventStop          StreamEventType = "stop"
	StreamEventError         StreamEventType = "error"
)

// StreamEvent is one event from the canonical stream.
type StreamEvent struct {
	Type StreamEventType

	// Content delta: text added to the assistant's reply.
	ContentDelta string

	// Tool call lifecycle: start defines (id, name), delta appends to args,
	// stop signals the call args are complete.
	ToolCallID         string
	ToolCallName       string
	ToolCallArgsDelta  string

	// Usage: prompt/completion tokens. Emitted at most once, near the end.
	Usage *Usage

	// Stop: reason the stream ended.
	StopReason string

	// Error: present when Type == StreamEventError.
	Error *ErrorInfo
}

type ErrorInfo struct {
	Code    string
	Message string
}

// StreamReader is the canonical event iterator. Provider adapters return one
// when streaming is requested. Recv returns io.EOF when the stream ends.
type StreamReader interface {
	Recv() (StreamEvent, error)
	Close() error
}
