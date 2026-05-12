package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/admin/maas-router/proxy/internal/provider"
)

const apiURL = "https://api.anthropic.com/v1/messages"

// httpClient has no Timeout — streaming requests can legitimately last
// several minutes. Cancellation is driven by the request context instead.
// Non-streaming requests should pass a context with timeout from the caller.
var httpClient = &http.Client{}

type Adapter struct {
	HTTP *http.Client
}

func New() *Adapter { return &Adapter{HTTP: httpClient} }

type messageBody struct {
	Model     string      `json:"model"`
	MaxTokens int         `json:"max_tokens"`
	System    string      `json:"system,omitempty"`
	Messages  []anthroMsg `json:"messages"`
	Stream    bool        `json:"stream,omitempty"`
}

type anthroMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messageResp struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (a *Adapter) buildBody(req provider.Request, stream bool) messageBody {
	body := messageBody{Model: req.Model, MaxTokens: req.MaxTokens, Stream: stream}
	for _, m := range req.Messages {
		if m.Role == "system" {
			body.System = m.Content
			continue
		}
		body.Messages = append(body.Messages, anthroMsg{Role: m.Role, Content: m.Content})
	}
	if body.MaxTokens == 0 {
		body.MaxTokens = 1024
	}
	return body
}

func (a *Adapter) request(ctx context.Context, body any, apiKey string) (*http.Response, error) {
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	return a.HTTP.Do(req)
}

func (a *Adapter) Send(ctx context.Context, req provider.Request, apiKey string) (*provider.Response, error) {
	if apiKey == "" {
		return nil, errors.New("anthropic api key not configured")
	}
	resp, err := a.request(ctx, a.buildBody(req, false), apiKey)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, b)
	}
	var out messageResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	var text string
	for _, c := range out.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	return &provider.Response{
		Content:          text,
		PromptTokens:     out.Usage.InputTokens,
		CompletionTokens: out.Usage.OutputTokens,
	}, nil
}

// SendStream parses Anthropic's SSE stream and emits canonical events.
func (a *Adapter) SendStream(ctx context.Context, req provider.Request, apiKey string) (provider.StreamReader, error) {
	if apiKey == "" {
		return nil, errors.New("anthropic api key not configured")
	}
	resp, err := a.request(ctx, a.buildBody(req, true), apiKey)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, b)
	}
	scanner := bufio.NewScanner(resp.Body)
	// Default scanner buffer is 64KB; a single content_block_delta with a long
	// code block can easily exceed that and silently truncate the stream.
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	return &anthropicStream{body: resp.Body, scanner: scanner}, nil
}

type anthropicStream struct {
	body    io.Closer
	scanner *bufio.Scanner
	usage   provider.Usage
	done    bool
}

func (s *anthropicStream) Recv() (provider.StreamEvent, error) {
	for s.scanner.Scan() {
		line := s.scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")

		var evt struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
			Message struct {
				Usage struct {
					InputTokens int `json:"input_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			continue
		}
		switch evt.Type {
		case "message_start":
			s.usage.PromptTokens = evt.Message.Usage.InputTokens
		case "content_block_delta":
			if evt.Delta.Type == "text_delta" && evt.Delta.Text != "" {
				return provider.StreamEvent{Type: "content", ContentDelta: evt.Delta.Text}, nil
			}
		case "message_delta":
			s.usage.CompletionTokens = evt.Usage.OutputTokens
		case "message_stop":
			s.done = true
			u := s.usage
			return provider.StreamEvent{Type: "stop", StopReason: "stop", Usage: &u}, io.EOF
		}
	}
	if err := s.scanner.Err(); err != nil {
		return provider.StreamEvent{}, err
	}
	return provider.StreamEvent{}, io.EOF
}

func (s *anthropicStream) Close() error { return s.body.Close() }
