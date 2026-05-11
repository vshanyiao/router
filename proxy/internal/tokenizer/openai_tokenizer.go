package tokenizer

import (
	"context"
	"fmt"

	"github.com/pkoukk/tiktoken-go"
)

// OpenAITokenizer uses tiktoken-go to count tokens for OpenAI chat models.
type OpenAITokenizer struct {
	enc *tiktoken.Tiktoken
}

func NewOpenAITokenizer() (*OpenAITokenizer, error) {
	enc, err := tiktoken.GetEncoding("o200k_base")
	if err != nil {
		return nil, fmt.Errorf("load tiktoken encoding: %w", err)
	}
	return &OpenAITokenizer{enc: enc}, nil
}

func (t *OpenAITokenizer) CountPromptTokens(_ context.Context, model string, messages []Message) (int, error) {
	const perMessage = 3
	const priming = 3

	total := priming
	for _, m := range messages {
		total += perMessage
		total += len(t.enc.Encode(m.Content, nil, nil))
		total += len(t.enc.Encode(m.Role, nil, nil))
	}
	return total, nil
}
