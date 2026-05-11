package tokenizer

import (
	"context"
	"fmt"
)

// Tokenizer counts tokens for a specific upstream provider/model family.
type Tokenizer interface {
	// CountPromptTokens returns an estimate (or exact count) of the tokens
	// a prompt would consume on the given upstream model.
	CountPromptTokens(ctx context.Context, model string, messages []Message) (int, error)
}

type Message struct {
	Role    string
	Content string
}

// Registry holds per-provider tokenizers and dispatches by upstream_provider.
type Registry struct {
	byProvider map[string]Tokenizer
}

func NewRegistry() *Registry {
	return &Registry{byProvider: map[string]Tokenizer{}}
}

func (r *Registry) Register(provider string, t Tokenizer) {
	r.byProvider[provider] = t
}

func (r *Registry) Get(provider string) (Tokenizer, error) {
	t, ok := r.byProvider[provider]
	if !ok {
		return nil, fmt.Errorf("no tokenizer registered for provider %q", provider)
	}
	return t, nil
}
