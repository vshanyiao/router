package tokenizer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// tokenizerHTTPClient bounds count_tokens API calls so a hung upstream
// cannot indefinitely block a request goroutine.
var tokenizerHTTPClient = &http.Client{Timeout: 90 * time.Second}

type AnthropicTokenizer struct {
	APIKey string
	HTTP   *http.Client
}

func NewAnthropicTokenizer(apiKey string) *AnthropicTokenizer {
	return &AnthropicTokenizer{APIKey: apiKey, HTTP: tokenizerHTTPClient}
}

type anthropicCountReq struct {
	Model    string             `json:"model"`
	System   string             `json:"system,omitempty"`
	Messages []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicCountResp struct {
	InputTokens int `json:"input_tokens"`
}

func (t *AnthropicTokenizer) CountPromptTokens(ctx context.Context, model string, messages []Message) (int, error) {
	if t.APIKey == "" {
		return 0, fmt.Errorf("anthropic api key not configured")
	}
	// Mirror the inference adapter's body shape: system goes to its own field,
	// only user/assistant turns appear in messages. Otherwise the count_tokens
	// API can reject non-alternating role sequences and the token count for a
	// system-as-user turn diverges from the system-as-system-field charge.
	body := anthropicCountReq{Model: model}
	for _, m := range messages {
		if m.Role == "system" {
			if body.System != "" {
				body.System += "\n\n"
			}
			body.System += m.Content
			continue
		}
		body.Messages = append(body.Messages, anthropicMessage{Role: m.Role, Content: m.Content})
	}
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages/count_tokens", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", t.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := t.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("anthropic count_tokens: status %d", resp.StatusCode)
	}
	var out anthropicCountResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.InputTokens, nil
}
