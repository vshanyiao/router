package openai

import (
	"context"
	"errors"
	"fmt"
	"io"

	openaisdk "github.com/sashabaranov/go-openai"
	"github.com/admin/maas-router/proxy/internal/provider"
)

type Adapter struct{}

func New() *Adapter { return &Adapter{} }

func (a *Adapter) Send(ctx context.Context, req provider.Request, apiKey string) (*provider.Response, error) {
	if apiKey == "" {
		return nil, errors.New("openai api key not configured")
	}
	client := openaisdk.NewClient(apiKey)

	var msgs []openaisdk.ChatCompletionMessage
	for _, m := range req.Messages {
		msgs = append(msgs, openaisdk.ChatCompletionMessage{Role: m.Role, Content: m.Content})
	}

	resp, err := client.CreateChatCompletion(ctx, openaisdk.ChatCompletionRequest{
		Model:     req.Model,
		Messages:  msgs,
		MaxTokens: req.MaxTokens,
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, errors.New("no choices returned")
	}
	return &provider.Response{
		Content:          resp.Choices[0].Message.Content,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
	}, nil
}

// streamReader wraps the OpenAI ChatCompletionStream and emits canonical events.
type streamReader struct {
	src              *openaisdk.ChatCompletionStream
	promptTokens     int
	completionTokens int
	done             bool
}

func (s *streamReader) Recv() (provider.StreamEvent, error) {
	if s.done {
		return provider.StreamEvent{}, fmt.Errorf("stream already closed: %w", io.EOF)
	}
	resp, err := s.src.Recv()
	if err == io.EOF {
		s.done = true
		ev := provider.StreamEvent{Type: "stop", StopReason: "stop", Usage: &provider.Usage{
			PromptTokens:     s.promptTokens,
			CompletionTokens: s.completionTokens,
		}}
		return ev, io.EOF
	}
	if err != nil {
		return provider.StreamEvent{}, err
	}
	if resp.Usage != nil {
		s.promptTokens = resp.Usage.PromptTokens
		s.completionTokens = resp.Usage.CompletionTokens
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Delta.Content == "" {
		return provider.StreamEvent{Type: "content", ContentDelta: ""}, nil
	}
	return provider.StreamEvent{
		Type:         "content",
		ContentDelta: resp.Choices[0].Delta.Content,
	}, nil
}

func (s *streamReader) Close() error { return s.src.Close() }

func (a *Adapter) SendStream(ctx context.Context, req provider.Request, apiKey string) (provider.StreamReader, error) {
	if apiKey == "" {
		return nil, errors.New("openai api key not configured")
	}
	client := openaisdk.NewClient(apiKey)
	var msgs []openaisdk.ChatCompletionMessage
	for _, m := range req.Messages {
		msgs = append(msgs, openaisdk.ChatCompletionMessage{Role: m.Role, Content: m.Content})
	}
	stream, err := client.CreateChatCompletionStream(ctx, openaisdk.ChatCompletionRequest{
		Model:         req.Model,
		Messages:      msgs,
		MaxTokens:     req.MaxTokens,
		Stream:        true,
		StreamOptions: &openaisdk.StreamOptions{IncludeUsage: true},
	})
	if err != nil {
		return nil, err
	}
	return &streamReader{src: stream}, nil
}
