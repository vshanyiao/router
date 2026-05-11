package openai

import (
	"context"
	"errors"

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
