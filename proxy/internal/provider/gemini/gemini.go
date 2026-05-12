package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/admin/maas-router/proxy/internal/provider"
)

const baseURL = "https://generativelanguage.googleapis.com/v1beta/models"

// httpClient has no Timeout — streaming requests can legitimately last
// several minutes. Cancellation is driven by the request context instead.
var httpClient = &http.Client{}

type Adapter struct{ HTTP *http.Client }

func New() *Adapter { return &Adapter{HTTP: httpClient} }

type genReq struct {
	Contents          []content `json:"contents"`
	SystemInstruction *content  `json:"systemInstruction,omitempty"`
	GenerationConfig  *genCfg   `json:"generationConfig,omitempty"`
}

type genCfg struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type part struct {
	Text string `json:"text"`
}

type genResp struct {
	Candidates []struct {
		Content      content `json:"content"`
		FinishReason string  `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
}

func buildBody(req provider.Request) genReq {
	body := genReq{}
	for _, m := range req.Messages {
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		if role == "system" {
			body.SystemInstruction = &content{Parts: []part{{Text: m.Content}}}
			continue
		}
		body.Contents = append(body.Contents, content{Role: role, Parts: []part{{Text: m.Content}}})
	}
	if req.MaxTokens > 0 {
		body.GenerationConfig = &genCfg{MaxOutputTokens: req.MaxTokens}
	}
	return body
}

func (a *Adapter) Send(ctx context.Context, req provider.Request, apiKey string) (*provider.Response, error) {
	if apiKey == "" {
		return nil, errors.New("gemini api key not configured")
	}
	url := fmt.Sprintf("%s/%s:generateContent", baseURL, req.Model)
	buf, _ := json.Marshal(buildBody(req))
	r, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(buf))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Goog-Api-Key", apiKey)
	resp, err := a.HTTP.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini: status %d: %s", resp.StatusCode, b)
	}
	var out genResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	var text string
	if len(out.Candidates) > 0 {
		for _, p := range out.Candidates[0].Content.Parts {
			text += p.Text
		}
	}
	return &provider.Response{
		Content:          text,
		PromptTokens:     out.UsageMetadata.PromptTokenCount,
		CompletionTokens: out.UsageMetadata.CandidatesTokenCount,
	}, nil
}

// SendStream uses :streamGenerateContent with SSE format.
func (a *Adapter) SendStream(ctx context.Context, req provider.Request, apiKey string) (provider.StreamReader, error) {
	if apiKey == "" {
		return nil, errors.New("gemini api key not configured")
	}
	url := fmt.Sprintf("%s/%s:streamGenerateContent?alt=sse", baseURL, req.Model)
	buf, _ := json.Marshal(buildBody(req))
	r, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(buf))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Goog-Api-Key", apiKey)
	resp, err := a.HTTP.Do(r)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("gemini: status %d: %s", resp.StatusCode, b)
	}
	return &geminiStream{body: resp.Body}, nil
}

type geminiStream struct {
	body  io.ReadCloser
	buf   []byte
	usage provider.Usage
	done  bool
}

func (s *geminiStream) Recv() (provider.StreamEvent, error) {
	if s.done {
		return provider.StreamEvent{}, io.EOF
	}
	tmp := make([]byte, 1024)
	for {
		// Look for complete SSE event in buffer (separated by \n\n)
		if idx := bytes.Index(s.buf, []byte("\n\n")); idx >= 0 {
			chunk := s.buf[:idx]
			s.buf = append([]byte{}, s.buf[idx+2:]...)

			var data []byte
			for _, line := range bytes.Split(chunk, []byte("\n")) {
				if bytes.HasPrefix(line, []byte("data: ")) {
					data = append(data, line[len("data: "):]...)
				}
			}
			if len(data) == 0 {
				continue
			}
			var partial genResp
			if err := json.Unmarshal(data, &partial); err != nil {
				continue
			}
			if partial.UsageMetadata.PromptTokenCount > 0 {
				s.usage.PromptTokens = partial.UsageMetadata.PromptTokenCount
			}
			if partial.UsageMetadata.CandidatesTokenCount > 0 {
				s.usage.CompletionTokens = partial.UsageMetadata.CandidatesTokenCount
			}
			if len(partial.Candidates) > 0 && len(partial.Candidates[0].Content.Parts) > 0 {
				text := partial.Candidates[0].Content.Parts[0].Text
				if text != "" {
					return provider.StreamEvent{Type: "content", ContentDelta: text}, nil
				}
			}
			continue
		}
		// Buffer doesn't have a complete event — read more from body
		n, err := s.body.Read(tmp)
		if n > 0 {
			s.buf = append(s.buf, tmp[:n]...)
		}
		if err != nil {
			if err == io.EOF {
				s.done = true
				u := s.usage
				return provider.StreamEvent{Type: "stop", StopReason: "stop", Usage: &u}, io.EOF
			}
			return provider.StreamEvent{}, err
		}
	}
}

func (s *geminiStream) Close() error { return s.body.Close() }
