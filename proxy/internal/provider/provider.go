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
