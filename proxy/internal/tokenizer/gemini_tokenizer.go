package tokenizer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type GeminiTokenizer struct {
	APIKey string
	HTTP   *http.Client
}

func NewGeminiTokenizer(apiKey string) *GeminiTokenizer {
	// Reuses the same 90s-bounded client defined in anthropic_tokenizer.go
	return &GeminiTokenizer{APIKey: apiKey, HTTP: tokenizerHTTPClient}
}

type geminiCountReq struct {
	Contents          []geminiContent `json:"contents"`
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiCountResp struct {
	TotalTokens int `json:"totalTokens"`
}

func (t *GeminiTokenizer) CountPromptTokens(ctx context.Context, model string, messages []Message) (int, error) {
	if t.APIKey == "" {
		return 0, fmt.Errorf("gemini api key not configured")
	}
	// Mirror the inference adapter's body shape: system goes to systemInstruction,
	// assistant maps to "model". A `user`-as-system would diverge from what the
	// real inference call charges and may also cause non-alternating-role errors.
	body := geminiCountReq{}
	var sysText string
	for _, m := range messages {
		if m.Role == "system" {
			if sysText != "" {
				sysText += "\n\n"
			}
			sysText += m.Content
			continue
		}
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		body.Contents = append(body.Contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: m.Content}},
		})
	}
	if sysText != "" {
		body.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: sysText}}}
	}
	buf, _ := json.Marshal(body)
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:countTokens", model)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goog-Api-Key", t.APIKey)
	resp, err := t.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("gemini countTokens: status %d", resp.StatusCode)
	}
	var out geminiCountResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.TotalTokens, nil
}
