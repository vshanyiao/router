package provider

import "context"

type Request struct {
	Model     string
	Messages  []Message
	MaxTokens int
}

type Message struct {
	Role    string
	Content string
}

type Response struct {
	Content          string
	PromptTokens     int
	CompletionTokens int
}

type Provider interface {
	Send(ctx context.Context, req Request, apiKey string) (*Response, error)
}

// StreamEvent is the provider-neutral streaming event emitted by adapters.
type StreamEvent struct {
	Type         string // "content" | "usage" | "stop" | "error"
	ContentDelta string // present when Type == "content"
	Usage        *Usage
	StopReason   string // "stop" | "length" | "cancelled"
	Error        *ErrorInfo
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

type ErrorInfo struct {
	Code    string
	Message string
}

// StreamReader is the canonical iterator returned by streaming providers.
type StreamReader interface {
	Recv() (StreamEvent, error) // io.EOF when stream ends
	Close() error
}

// StreamingProvider extends Provider with streaming support.
type StreamingProvider interface {
	Provider
	SendStream(ctx context.Context, req Request, apiKey string) (StreamReader, error)
}
