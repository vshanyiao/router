package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/admin/maas-router/proxy/internal/canonical"
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

// ---------------------------------------------------------------------------
// CanonicalProvider implementation
// ---------------------------------------------------------------------------

// canonicalReqBody is the Anthropic /v1/messages wire format with full support
// for tools, vision, and multi-modal content.
type canonicalReqBody struct {
	Model      string           `json:"model"`
	MaxTokens  int              `json:"max_tokens"`
	System     string           `json:"system,omitempty"`
	Messages   []canonicalMsg   `json:"messages"`
	Tools      []anthroTool     `json:"tools,omitempty"`
	ToolChoice *anthroToolChoice `json:"tool_choice,omitempty"`
	Stream     bool             `json:"stream,omitempty"`
}

type canonicalMsg struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

type anthroTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthroToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// anthroContentBlock is the Anthropic wire content block (used in both
// requests and responses).
type anthroContentBlock struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	ID         string          `json:"id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	ToolUseID  string          `json:"tool_use_id,omitempty"`
	Content    string          `json:"content,omitempty"`
	Source     *anthroImgSrc   `json:"source,omitempty"`
}

type anthroImgSrc struct {
	Type      string `json:"type"`                 // "url" or "base64"
	URL       string `json:"url,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`       // base64-encoded
}

// canonicalRespBody is the Anthropic /v1/messages response wire format.
type canonicalRespBody struct {
	Content    []anthroContentBlock `json:"content"`
	StopReason string               `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// buildCanonicalBody converts a canonical.Request to the Anthropic wire body.
func (a *Adapter) buildCanonicalBody(req canonical.Request, stream bool) (canonicalReqBody, error) {
	body := canonicalReqBody{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		System:    req.System,
		Stream:    stream,
	}
	if body.MaxTokens == 0 {
		body.MaxTokens = 1024
	}

	// tools
	if req.ToolChoice.Mode != canonical.ToolChoiceNone && len(req.Tools) > 0 {
		for _, t := range req.Tools {
			body.Tools = append(body.Tools, anthroTool{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
			})
		}
	}

	// tool_choice
	switch req.ToolChoice.Mode {
	case canonical.ToolChoiceAny:
		body.ToolChoice = &anthroToolChoice{Type: "any"}
	case canonical.ToolChoiceSpecific:
		body.ToolChoice = &anthroToolChoice{Type: "tool", Name: req.ToolChoice.ToolName}
	// auto and none: omit (Anthropic defaults to auto when tools are present)
	}

	// messages
	for _, m := range req.Messages {
		role := string(m.Role)
		// Anthropic folds RoleTool into user messages with tool_result blocks.
		if m.Role == canonical.RoleTool {
			role = "user"
		}

		var blocks []json.RawMessage
		for _, cb := range m.Content {
			b, err := marshalContentBlock(cb)
			if err != nil {
				return canonicalReqBody{}, err
			}
			if b == nil {
				continue // skip (e.g. tool_result without tool_use_id)
			}
			blocks = append(blocks, b)
		}
		if len(blocks) == 0 {
			continue
		}
		body.Messages = append(body.Messages, canonicalMsg{Role: role, Content: blocks})
	}

	return body, nil
}

// marshalContentBlock converts a canonical.ContentBlock to raw JSON for the
// Anthropic wire format. Returns nil if the block should be skipped.
func marshalContentBlock(cb canonical.ContentBlock) (json.RawMessage, error) {
	switch cb.Type {
	case canonical.BlockText:
		return json.Marshal(map[string]string{"type": "text", "text": cb.Text})

	case canonical.BlockImage:
		src := &anthroImgSrc{}
		if cb.ImageURL != "" {
			src.Type = "url"
			src.URL = cb.ImageURL
		} else {
			src.Type = "base64"
			src.MediaType = cb.MediaType
			src.Data = encodeBase64(cb.ImageData)
		}
		return json.Marshal(anthroContentBlock{Type: "image", Source: src})

	case canonical.BlockToolUse:
		return json.Marshal(anthroContentBlock{
			Type:  "tool_use",
			ID:    cb.ToolUseID,
			Name:  cb.ToolName,
			Input: cb.ToolInput,
		})

	case canonical.BlockToolResult:
		if cb.ToolUseID == "" {
			return nil, nil // skip tool_result blocks without a tool_use_id
		}
		return json.Marshal(anthroContentBlock{
			Type:      "tool_result",
			ToolUseID: cb.ToolUseID,
			Content:   cb.ToolResult,
		})
	}
	return nil, nil
}

func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// SendCanonical implements provider.CanonicalProvider.
func (a *Adapter) SendCanonical(ctx context.Context, req canonical.Request, apiKey string) (*canonical.Response, error) {
	if apiKey == "" {
		return nil, errors.New("anthropic api key not configured")
	}
	body, err := a.buildCanonicalBody(req, false)
	if err != nil {
		return nil, err
	}
	resp, err := a.request(ctx, body, apiKey)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, b)
	}
	var out canonicalRespBody
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	var blocks []canonical.ContentBlock
	for _, c := range out.Content {
		switch c.Type {
		case "text":
			blocks = append(blocks, canonical.TextBlock(c.Text))
		case "tool_use":
			blocks = append(blocks, canonical.ToolUseBlock(c.ID, c.Name, c.Input))
		}
	}

	stopReason := mapStopReason(out.StopReason)
	return &canonical.Response{
		Content:    blocks,
		StopReason: stopReason,
		Usage: canonical.Usage{
			PromptTokens:     out.Usage.InputTokens,
			CompletionTokens: out.Usage.OutputTokens,
		},
	}, nil
}

// mapStopReason converts the Anthropic stop_reason to the canonical form.
func mapStopReason(r string) string {
	switch r {
	case "tool_use":
		return "tool_use"
	case "max_tokens":
		return "length"
	default:
		return "stop"
	}
}

// SendCanonicalStream implements provider.CanonicalProvider.
func (a *Adapter) SendCanonicalStream(ctx context.Context, req canonical.Request, apiKey string) (canonical.StreamReader, error) {
	if apiKey == "" {
		return nil, errors.New("anthropic api key not configured")
	}
	body, err := a.buildCanonicalBody(req, true)
	if err != nil {
		return nil, err
	}
	resp, err := a.request(ctx, body, apiKey)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, b)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	return &canonicalStream{body: resp.Body, scanner: scanner}, nil
}

// canonicalStream reads Anthropic SSE events and emits canonical.StreamEvents.
type canonicalStream struct {
	body         io.Closer
	scanner      *bufio.Scanner
	pendingEvent string // last seen "event: <name>" line

	inputTokens  int
	outputTokens int
	stopReason   string

	// current tool-use block state: populated on content_block_start tool_use,
	// used when emitting StreamEventToolCallDelta for input_json_delta events.
	currentToolID string

	done bool
}

func (s *canonicalStream) Recv() (canonical.StreamEvent, error) {
	if s.done {
		return canonical.StreamEvent{}, io.EOF
	}
	for s.scanner.Scan() {
		line := s.scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			s.pendingEvent = strings.TrimPrefix(line, "event: ")
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")

		switch s.pendingEvent {
		case "message_start":
			var e struct {
				Message struct {
					Usage struct {
						InputTokens int `json:"input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(payload), &e); err == nil {
				s.inputTokens = e.Message.Usage.InputTokens
			}

		case "content_block_start":
			var e struct {
				ContentBlock struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(payload), &e); err != nil {
				continue
			}
			if e.ContentBlock.Type == "tool_use" {
				s.currentToolID = e.ContentBlock.ID
				return canonical.StreamEvent{
					Type:         canonical.StreamEventToolCallStart,
					ToolCallID:   e.ContentBlock.ID,
					ToolCallName: e.ContentBlock.Name,
				}, nil
			}
			// text block start — nothing to emit, just clear currentToolID
			s.currentToolID = ""

		case "content_block_delta":
			var e struct {
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(payload), &e); err != nil {
				continue
			}
			switch e.Delta.Type {
			case "text_delta":
				if e.Delta.Text != "" {
					return canonical.StreamEvent{
						Type:         canonical.StreamEventContent,
						ContentDelta: e.Delta.Text,
					}, nil
				}
			case "input_json_delta":
				return canonical.StreamEvent{
					Type:              canonical.StreamEventToolCallDelta,
					ToolCallID:        s.currentToolID,
					ToolCallArgsDelta: e.Delta.PartialJSON,
				}, nil
			}

		case "content_block_stop":
			if s.currentToolID != "" {
				id := s.currentToolID
				s.currentToolID = ""
				return canonical.StreamEvent{
					Type:       canonical.StreamEventToolCallStop,
					ToolCallID: id,
				}, nil
			}

		case "message_delta":
			var e struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(payload), &e); err == nil {
				s.outputTokens = e.Usage.OutputTokens
				s.stopReason = mapStopReason(e.Delta.StopReason)
			}

		case "message_stop":
			s.done = true
			return canonical.StreamEvent{
				Type:       canonical.StreamEventStop,
				StopReason: s.stopReason,
				Usage: &canonical.Usage{
					PromptTokens:     s.inputTokens,
					CompletionTokens: s.outputTokens,
				},
			}, nil
		}
	}
	if err := s.scanner.Err(); err != nil {
		return canonical.StreamEvent{}, err
	}
	return canonical.StreamEvent{}, io.EOF
}

func (s *canonicalStream) Close() error { return s.body.Close() }
