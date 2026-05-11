package tokenizer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAITokenizer_CountsTokens(t *testing.T) {
	tok, err := NewOpenAITokenizer()
	require.NoError(t, err)

	ctx := context.Background()
	count, err := tok.CountPromptTokens(ctx, "gpt-4o", []Message{
		{Role: "user", Content: "Hello, world!"},
	})
	require.NoError(t, err)
	assert.Greater(t, count, 5)
	assert.Less(t, count, 20)
}

func TestOpenAITokenizer_LongerPromptIsMoreTokens(t *testing.T) {
	tok, err := NewOpenAITokenizer()
	require.NoError(t, err)

	ctx := context.Background()
	short, _ := tok.CountPromptTokens(ctx, "gpt-4o", []Message{{Role: "user", Content: "Hi"}})
	long, _ := tok.CountPromptTokens(ctx, "gpt-4o", []Message{{Role: "user", Content: "Hi " + repeat("blah ", 100)}})
	assert.Greater(t, long, short)
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
