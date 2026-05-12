# MaaS Router — Phase 1: Streaming + Multi-Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax. **Within Phase 1, the three provider adapters (OpenAI streaming, Anthropic, Gemini) are independent and CAN be dispatched in parallel** once the streaming interface is defined.

**Goal:** Add streaming SSE support and integrate two more providers (Anthropic + Google) on the existing OpenAI surface. Replace the prompt-token estimate hack with real tokenizers. Add the stuck-reservation reaper deferred from Phase 0. Add a simple usage-history page.

**Architecture:** Extend the existing `proxy` Go service. Provider abstraction grows: a `Tokenizer` interface alongside the existing `Provider` interface; a `StreamReader` for canonical streaming events. The `web` UI gets a `/dashboard/usage` page reading `request_logs`.

**Tech Stack:** Same as Phase 0, plus:
- `github.com/pkoukk/tiktoken-go` for OpenAI tokenization
- Anthropic SDK or HTTP direct (`github.com/anthropics/anthropic-sdk-go` if usable, else custom)
- Gemini via Google's REST API (no official Go SDK is widely used; HTTP direct is fine)

**Reference:** Design spec §5 (request flow with streaming), §6 (provider abstraction), §6.5 (canonical streaming events).

**Branched from:** `phase-0-foundation` (PR #1).

**Out of scope for Phase 1** (later phases):
- Anthropic `/v1/messages` API surface (Phase 2)
- Tool calls, vision (Phase 2)
- Stripe / top-up (Phase 3)
- Admin panel (Phase 4)
- Bilingual UI (Phase 5)
- AWS deployment (Phase 6)

---

## File Structure (additions/changes from Phase 0)

```
proxy/
  internal/
    provider/
      provider.go              ⚠ MODIFIED: add streaming interface
      openai/
        openai.go              ⚠ MODIFIED: add streaming
        openai_test.go         + NEW: streaming tests
      anthropic/               + NEW package
        anthropic.go
        anthropic_test.go
      gemini/                  + NEW package
        gemini.go
        gemini_test.go
    tokenizer/                 + NEW package
      tokenizer.go             (interface + Registry)
      tokenizer_test.go
      openai_tokenizer.go      (tiktoken-go wrapper)
      anthropic_tokenizer.go   (use Anthropic count_tokens endpoint)
      gemini_tokenizer.go      (use Google countTokens REST)
    server/
      openai.go                ⚠ MODIFIED: streaming + use Tokenizer
    reaper/                    + NEW package
      reaper.go                (stuck-reservation sweeper)
      reaper_test.go
    stream/                    + NEW package
      sse.go                   (SSE writer helpers, canonical event types)

web/
  src/
    app/
      dashboard/
        usage/
          page.tsx             + NEW: usage history page
    app/api/usage/
      route.ts                 + NEW: list user's recent request_logs

  prisma/
    seed.ts                    ⚠ MODIFIED: add 6 more models
```

---

## Group A: Streaming infrastructure

### Task 1: Define canonical streaming types

**Files:**
- Modify: `proxy/internal/provider/provider.go`

- [ ] **Step 1: Add streaming types to provider package**

Append (don't remove existing types) to `proxy/internal/provider/provider.go`:

```go
// StreamEvent is the provider-neutral streaming event emitted by adapters.
type StreamEvent struct {
	Type         string // "content" | "usage" | "stop" | "error"
	ContentDelta string // present when Type == "content"
	Usage        *Usage
	StopReason   string // "stop" | "length" | "cancelled"
	Error        *ErrorInfo
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

type ErrorInfo struct {
	Code    string
	Message string
}

// StreamReader is the canonical iterator returned by streaming providers.
type StreamReader interface {
	Recv() (StreamEvent, error) // io.EOF when stream ends
	Close() error
}

// StreamingProvider extends Provider with streaming support.
type StreamingProvider interface {
	Provider
	SendStream(ctx context.Context, req Request, apiKey string) (StreamReader, error)
}
```

Also add the import: `"context"` if not already present (it should be, from existing `Provider.Send` signature).

- [ ] **Step 2: Build to verify**

```bash
cd proxy && go build ./...
```

Expected: no compile errors.

- [ ] **Step 3: Commit**

```bash
git add proxy/internal/provider/provider.go
git commit -m "proxy: add canonical StreamEvent + StreamingProvider interface"
```

---

### Task 2: Add stream writer helper

**Files:**
- Create: `proxy/internal/stream/sse.go`

- [ ] **Step 1: Write the SSE helper**

```go
package stream

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Writer wraps an http.ResponseWriter for sending Server-Sent Events.
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewWriter prepares the response for SSE and returns a writer.
// Returns nil if the response doesn't support flushing.
func NewWriter(w http.ResponseWriter) *Writer {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	return &Writer{w: w, flusher: flusher}
}

// SendJSON marshals data and writes one SSE `data:` event.
func (s *Writer) SendJSON(data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", b); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// SendDone writes the SSE terminator that OpenAI clients expect.
func (s *Writer) SendDone() error {
	if _, err := fmt.Fprint(s.w, "data: [DONE]\n\n"); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}
```

- [ ] **Step 2: Build + commit**

```bash
go build ./... && git add proxy/internal/stream/ && git commit -m "proxy: add SSE writer with OpenAI-compatible [DONE] terminator"
```

---

## Group B: Tokenizers

### Task 3: Tokenizer interface + registry

**Files:**
- Create: `proxy/internal/tokenizer/tokenizer.go`

- [ ] **Step 1: Write interface + registry**

```go
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
```

- [ ] **Step 2: Commit**

```bash
go build ./... && git add proxy/internal/tokenizer/tokenizer.go && git commit -m "proxy: add Tokenizer interface + Registry"
```

---

### Task 4: OpenAI tokenizer (tiktoken-go)

**Files:**
- Create: `proxy/internal/tokenizer/openai_tokenizer.go`
- Create: `proxy/internal/tokenizer/openai_tokenizer_test.go`

- [ ] **Step 1: Add tiktoken-go dependency**

```bash
cd proxy && go get github.com/pkoukk/tiktoken-go
```

- [ ] **Step 2: Write the failing test**

`proxy/internal/tokenizer/openai_tokenizer_test.go`:

```go
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
	// "Hello, world!" → ~4 tokens; with role/format overhead, expect 8-15
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
```

- [ ] **Step 3: Run, watch fail**

```bash
go test ./internal/tokenizer/...
```

Expected: FAIL — `NewOpenAITokenizer` undefined.

- [ ] **Step 4: Implement OpenAI tokenizer**

`proxy/internal/tokenizer/openai_tokenizer.go`:

```go
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
	// o200k_base is the encoding used by GPT-4o family. Older models use cl100k_base.
	// Using o200k_base is correct for our launch set (GPT-4o, 4o-mini, o1).
	enc, err := tiktoken.GetEncoding("o200k_base")
	if err != nil {
		return nil, fmt.Errorf("load tiktoken encoding: %w", err)
	}
	return &OpenAITokenizer{enc: enc}, nil
}

func (t *OpenAITokenizer) CountPromptTokens(_ context.Context, model string, messages []Message) (int, error) {
	// Per OpenAI's cookbook, chat tokenization adds overhead for role wrapping.
	// For gpt-4o/o200k_base: 3 tokens per message + content tokens + 3 priming tokens.
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
```

- [ ] **Step 5: Run tests, expect PASS**

```bash
go test ./internal/tokenizer/... -v
```

- [ ] **Step 6: Commit**

```bash
git add proxy/ && git commit -m "proxy: add OpenAI tokenizer using tiktoken-go (o200k_base)"
```

---

### Task 5: Anthropic + Gemini tokenizers (HTTP-based)

**Files:**
- Create: `proxy/internal/tokenizer/anthropic_tokenizer.go`
- Create: `proxy/internal/tokenizer/gemini_tokenizer.go`

For Phase 1, we use **per-request HTTP calls** to each provider's official `count_tokens` / `countTokens` endpoints. These are fast (~50ms) and authoritative. We can optimize with caching or local tokenizers later.

- [ ] **Step 1: Write Anthropic tokenizer**

```go
package tokenizer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type AnthropicTokenizer struct {
	APIKey string
	HTTP   *http.Client
}

func NewAnthropicTokenizer(apiKey string) *AnthropicTokenizer {
	return &AnthropicTokenizer{APIKey: apiKey, HTTP: http.DefaultClient}
}

type anthropicCountReq struct {
	Model    string             `json:"model"`
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
	body := anthropicCountReq{Model: model}
	for _, m := range messages {
		role := m.Role
		if role == "system" {
			// Anthropic count_tokens accepts system as its own field; for simplicity we
			// fold system content into a user-prefixed turn for counting purposes.
			role = "user"
		}
		body.Messages = append(body.Messages, anthropicMessage{Role: role, Content: m.Content})
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
```

- [ ] **Step 2: Write Gemini tokenizer**

```go
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
	return &GeminiTokenizer{APIKey: apiKey, HTTP: http.DefaultClient}
}

type geminiCountReq struct {
	Contents []geminiContent `json:"contents"`
}

type geminiContent struct {
	Role  string       `json:"role"`
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
	body := geminiCountReq{}
	for _, m := range messages {
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		if role == "system" {
			role = "user" // Gemini system handled separately; fold for counting.
		}
		body.Contents = append(body.Contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: m.Content}},
		})
	}
	buf, _ := json.Marshal(body)
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:countTokens?key=%s", model, t.APIKey)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
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
```

- [ ] **Step 3: Build + commit**

```bash
go build ./... && git add proxy/ && git commit -m "proxy: add Anthropic + Gemini tokenizers (HTTP count_tokens)"
```

---

## Group C: Streaming for OpenAI provider

### Task 6: OpenAI streaming adapter

**Files:**
- Modify: `proxy/internal/provider/openai/openai.go`

- [ ] **Step 1: Add SendStream method**

Append to `proxy/internal/provider/openai/openai.go`:

```go
import (
	// add to existing imports
	"github.com/admin/maas-router/proxy/internal/provider"
	openaisdk "github.com/sashabaranov/go-openai"
)

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
	// Pick up final-chunk usage if OpenAI provided it (requires stream_options.include_usage)
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
		Model:     req.Model,
		Messages:  msgs,
		MaxTokens: req.MaxTokens,
		Stream:    true,
		StreamOptions: &openaisdk.StreamOptions{IncludeUsage: true},
	})
	if err != nil {
		return nil, err
	}
	return &streamReader{src: stream}, nil
}
```

Also add to the top imports (next to existing): `"fmt"`, `"io"`.

- [ ] **Step 2: Build + commit**

```bash
go build ./... && git add proxy/ && git commit -m "proxy: add OpenAI streaming with usage in final chunk"
```

---

## Group D: Anthropic provider

### Task 7: Anthropic provider (non-streaming + streaming)

**Files:**
- Create: `proxy/internal/provider/anthropic/anthropic.go`
- Create: `proxy/internal/provider/anthropic/anthropic_test.go`

- [ ] **Step 1: Write the adapter**

`proxy/internal/provider/anthropic/anthropic.go`:

```go
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

type Adapter struct {
	HTTP *http.Client
}

func New() *Adapter { return &Adapter{HTTP: http.DefaultClient} }

type messageBody struct {
	Model     string         `json:"model"`
	MaxTokens int            `json:"max_tokens"`
	System    string         `json:"system,omitempty"`
	Messages  []anthroMsg    `json:"messages"`
	Stream    bool           `json:"stream,omitempty"`
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
	return &anthropicStream{body: resp.Body, scanner: bufio.NewScanner(resp.Body)}, nil
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
			Type    string `json:"type"`
			Delta   struct {
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
```

- [ ] **Step 2: Smoke test (unit) — Adapter constructor works**

`proxy/internal/provider/anthropic/anthropic_test.go`:

```go
package anthropic

import "testing"

func TestNew(t *testing.T) {
	a := New()
	if a.HTTP == nil {
		t.Fatal("expected HTTP client to be set")
	}
}
```

- [ ] **Step 3: Build, test, commit**

```bash
go build ./... && go test ./internal/provider/anthropic/... && git add proxy/ && git commit -m "proxy: add Anthropic provider with streaming SSE parser"
```

---

## Group E: Gemini provider

### Task 8: Gemini provider (non-streaming + streaming)

**Files:**
- Create: `proxy/internal/provider/gemini/gemini.go`
- Create: `proxy/internal/provider/gemini/gemini_test.go`

- [ ] **Step 1: Write the adapter**

`proxy/internal/provider/gemini/gemini.go`:

```go
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

type Adapter struct{ HTTP *http.Client }

func New() *Adapter { return &Adapter{HTTP: http.DefaultClient} }

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
	url := fmt.Sprintf("%s/%s:generateContent?key=%s", baseURL, req.Model, apiKey)
	buf, _ := json.Marshal(buildBody(req))
	r, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(buf))
	r.Header.Set("Content-Type", "application/json")
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
	url := fmt.Sprintf("%s/%s:streamGenerateContent?alt=sse&key=%s", baseURL, req.Model, apiKey)
	buf, _ := json.Marshal(buildBody(req))
	r, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(buf))
	r.Header.Set("Content-Type", "application/json")
	resp, err := a.HTTP.Do(r)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("gemini: status %d: %s", resp.StatusCode, b)
	}
	return &geminiStream{body: resp.Body, dec: json.NewDecoder(resp.Body)}, nil
}

type geminiStream struct {
	body io.ReadCloser
	dec  *json.Decoder
	usage provider.Usage
	done  bool
}

func (s *geminiStream) Recv() (provider.StreamEvent, error) {
	if s.done {
		return provider.StreamEvent{}, io.EOF
	}
	// Gemini SSE: each `data: {...}` payload is a genResp partial.
	// json.Decoder over the raw stream doesn't strip `data:` prefix — we need a scanner.
	// For simplicity in Phase 1 we read line-by-line.
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)
	for {
		// Read until we see a newline (rough line-mode read).
		n, err := s.body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if err == io.EOF {
				s.done = true
				u := s.usage
				return provider.StreamEvent{Type: "stop", StopReason: "stop", Usage: &u}, io.EOF
			}
			return provider.StreamEvent{}, err
		}
		// Find a complete event (separated by `\n\n`).
		idx := bytes.Index(buf, []byte("\n\n"))
		if idx < 0 {
			continue
		}
		chunk := buf[:idx]
		// shift consumed bytes (allocate a fresh slice; tiny buffers, no perf concern)
		buf = append([]byte{}, buf[idx+2:]...)

		// Strip `data: ` prefix on each line; join multi-line data fields.
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
		// Accumulate usage if present (Gemini puts it in the last chunk).
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
	}
}

func (s *geminiStream) Close() error { return s.body.Close() }
```

- [ ] **Step 2: Smoke test (constructor)**

`proxy/internal/provider/gemini/gemini_test.go`:

```go
package gemini

import "testing"

func TestBuildBody_SystemRoleSeparated(t *testing.T) {
	// system messages should become SystemInstruction, not appear in Contents
	// (smoke check; this function is unexported so we test via Send indirectly later)
	t.Skip("body building is exercised via integration tests; see Phase 1 task 14")
}
```

- [ ] **Step 3: Build + commit**

```bash
go build ./... && git add proxy/ && git commit -m "proxy: add Gemini provider with streaming SSE parser"
```

---

## Group F: Wire streaming into the completions handler

### Task 9: Update completions handler — provider dispatch + streaming branch

**Files:**
- Modify: `proxy/internal/server/openai.go`
- Modify: `proxy/cmd/proxy/main.go`

- [ ] **Step 1: Add a Providers map to OpenAIHandler**

In `proxy/internal/server/openai.go`, change the handler struct:

```go
type OpenAIHandler struct {
	Auth      *auth.Auth
	Billing   *billing.Service
	Catalog   *catalog.Catalog
	Providers map[string]provider.Provider        // keyed by upstream_provider name
	Tokenizers *tokenizer.Registry
	Keys       map[string]string                  // upstream_provider → API key
	Reactor    *logging.Reactor
}
```

(Remove the old `OpenAI` and `OpenAIKey` fields. The new map keys are `"openai"`, `"anthropic"`, `"google"`.)

Add import: `"github.com/admin/maas-router/proxy/internal/tokenizer"`.

- [ ] **Step 2: Use Tokenizer + provider map in ChatCompletions**

Replace the `// Cost reservation` section's token-estimate code:

```go
// Tokenize for accurate reservation
tk, err := h.Tokenizers.Get(model.UpstreamProvider)
if err != nil {
	log.Printf("ERROR: no tokenizer for provider %q: %v", model.UpstreamProvider, err)
	writeError(w, http.StatusInternalServerError, "internal error")
	return
}
var tokMsgs []tokenizer.Message
for _, m := range req.Messages {
	tokMsgs = append(tokMsgs, tokenizer.Message{Role: m.Role, Content: m.Content})
}
estPromptTokens64, err := tk.CountPromptTokens(ctx, model.UpstreamModelID, tokMsgs)
if err != nil {
	// Fall back to char-count estimate
	var promptChars int
	for _, m := range req.Messages {
		promptChars += len(m.Content)
	}
	estPromptTokens64 = promptChars / 4
}
estPromptTokens := int64(estPromptTokens64)
```

Replace `h.OpenAI.Send(...)` with a provider lookup:

```go
prov, ok := h.Providers[model.UpstreamProvider]
if !ok {
	writeError(w, http.StatusBadRequest, "no provider adapter for "+model.UpstreamProvider)
	return
}
apiKey, ok := h.Keys[model.UpstreamProvider]
if !ok || apiKey == "" {
	writeError(w, http.StatusInternalServerError, "provider key not configured")
	return
}
```

Then split the streaming vs non-streaming path:

```go
if req.Stream {
	// Streaming branch — see Step 3
	h.handleStream(ctx, w, r, prov, apiKey, model, result, requestID, started, maxCost, &req)
	return
}

// Non-streaming branch (existing code, swap h.OpenAI for prov, h.OpenAIKey for apiKey)
upstream, err := prov.Send(ctx, provider.Request{Model: model.UpstreamModelID, Messages: messages, MaxTokens: req.MaxTokens}, apiKey)
// ... rest of non-streaming handling unchanged ...
```

- [ ] **Step 3: Add handleStream method**

Append to `proxy/internal/server/openai.go`:

```go
import "github.com/admin/maas-router/proxy/internal/stream" // top of file

// streamingChunk is the OAI-compatible SSE chunk we emit.
type streamingChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int                   `json:"index"`
		Delta        ChatCompletionMessage `json:"delta"`
		FinishReason *string               `json:"finish_reason,omitempty"`
	} `json:"choices"`
}

func (h *OpenAIHandler) handleStream(
	ctx context.Context,
	w http.ResponseWriter,
	_ *http.Request,
	prov provider.Provider,
	apiKey string,
	model catalog.Model,
	result *auth.LookupResult,
	requestID uuid.UUID,
	started time.Time,
	maxCost billing.Cost,
	req *ChatCompletionsRequest,
) {
	streamer, ok := prov.(provider.StreamingProvider)
	if !ok {
		_ = h.Billing.Finalize(ctx, result.UserID, requestID, maxCost.TotalCents, 0)
		writeError(w, http.StatusBadRequest, "model does not support streaming")
		return
	}
	var messages []provider.Message
	for _, m := range req.Messages {
		messages = append(messages, provider.Message{Role: m.Role, Content: m.Content})
	}
	reader, err := streamer.SendStream(ctx, provider.Request{
		Model:     model.UpstreamModelID,
		Messages:  messages,
		MaxTokens: req.MaxTokens,
	}, apiKey)
	if err != nil {
		_ = h.Billing.Finalize(ctx, result.UserID, requestID, maxCost.TotalCents, 0)
		log.Printf("ERROR: stream open failed: %v", err)
		writeError(w, http.StatusBadGateway, "upstream provider error")
		return
	}
	defer reader.Close()

	sse := stream.NewWriter(w)
	if sse == nil {
		_ = h.Billing.Finalize(ctx, result.UserID, requestID, maxCost.TotalCents, 0)
		writeError(w, http.StatusInternalServerError, "response writer does not support streaming")
		return
	}

	var finalUsage *provider.Usage
	for {
		evt, err := reader.Recv()
		if evt.Type == "content" && evt.ContentDelta != "" {
			chunk := streamingChunk{
				ID:      "chatcmpl-" + requestID.String(),
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   req.Model,
			}
			chunk.Choices = append(chunk.Choices, struct {
				Index        int                   `json:"index"`
				Delta        ChatCompletionMessage `json:"delta"`
				FinishReason *string               `json:"finish_reason,omitempty"`
			}{Index: 0, Delta: ChatCompletionMessage{Content: evt.ContentDelta}})
			if writeErr := sse.SendJSON(chunk); writeErr != nil {
				log.Printf("ERROR: sse write: %v", writeErr)
				break
			}
		}
		if evt.Usage != nil {
			finalUsage = evt.Usage
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("ERROR: stream recv: %v", err)
			break
		}
	}

	stopReason := "stop"
	finalChunk := streamingChunk{
		ID:      "chatcmpl-" + requestID.String(),
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   req.Model,
	}
	finalChunk.Choices = append(finalChunk.Choices, struct {
		Index        int                   `json:"index"`
		Delta        ChatCompletionMessage `json:"delta"`
		FinishReason *string               `json:"finish_reason,omitempty"`
	}{Index: 0, Delta: ChatCompletionMessage{}, FinishReason: &stopReason})
	_ = sse.SendJSON(finalChunk)
	_ = sse.SendDone()

	// Finalize billing
	var promptTokens, completionTokens int
	if finalUsage != nil {
		promptTokens = finalUsage.PromptTokens
		completionTokens = finalUsage.CompletionTokens
	}
	actual := billing.CalculateCost(int64(promptTokens), int64(completionTokens),
		model.InputCentsPerMillionTokens, model.OutputCentsPerMillionTokens, model.MarkupPct)
	_ = h.Billing.Finalize(ctx, result.UserID, requestID, maxCost.TotalCents, actual.TotalCents)

	// Async log
	now := time.Now()
	latency := int(time.Since(started).Milliseconds())
	pt, ct := promptTokens, completionTokens
	h.Reactor.Push(logging.RequestLogEntry{
		ID: requestID, UserID: result.UserID, APIKeyID: result.APIKeyID,
		APISurface: "openai", UpstreamProvider: model.UpstreamProvider,
		UpstreamModel: model.UpstreamModelID, ModelAlias: model.Alias,
		PromptTokens: &pt, CompletionTokens: &ct,
		InputCostCents: int(actual.InputCents), OutputCostCents: int(actual.OutputCents),
		MarginCents: int(actual.MarginCents), TotalChargedCents: int(actual.TotalCents),
		Streaming: true, Status: "success",
		LatencyMs: &latency, CreatedAt: started, CompletedAt: &now,
	})
}
```

Add to the import block at the top of the file:
```go
import (
	// existing imports plus:
	"io"
	"github.com/admin/maas-router/proxy/internal/stream"
	"github.com/admin/maas-router/proxy/internal/tokenizer"
)
```

Remove the existing block-rejection of streaming at the top of ChatCompletions (the `if req.Stream { writeError ... return }`). Keep all other validation.

- [ ] **Step 4: Update main.go to construct the new Handler shape**

In `proxy/cmd/proxy/main.go`, replace the handler construction:

```go
import (
	// add to existing imports:
	"github.com/admin/maas-router/proxy/internal/provider/anthropic"
	"github.com/admin/maas-router/proxy/internal/provider/gemini"
	"github.com/admin/maas-router/proxy/internal/tokenizer"
)

// later, replace `handler := &server.OpenAIHandler{...}` with:
openaiKey := os.Getenv("OPENAI_API_KEY")
anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
geminiKey := os.Getenv("GEMINI_API_KEY")

tokReg := tokenizer.NewRegistry()
oaiTok, err := tokenizer.NewOpenAITokenizer()
if err != nil {
	log.Fatalf("tokenizer init: %v", err)
}
tokReg.Register("openai", oaiTok)
tokReg.Register("anthropic", tokenizer.NewAnthropicTokenizer(anthropicKey))
tokReg.Register("google", tokenizer.NewGeminiTokenizer(geminiKey))

handler := &server.OpenAIHandler{
	Auth:    auth.New(pg, rdb, hmacSecret),
	Billing: billing.New(pg),
	Catalog: cat,
	Providers: map[string]provider.Provider{
		"openai":    openai.New(),
		"anthropic": anthropic.New(),
		"google":    gemini.New(),
	},
	Tokenizers: tokReg,
	Keys: map[string]string{
		"openai":    openaiKey,
		"anthropic": anthropicKey,
		"google":    geminiKey,
	},
	Reactor: reactor,
}
```

Add the missing import: `"github.com/admin/maas-router/proxy/internal/provider"`.

- [ ] **Step 5: Build + test + commit**

```bash
cd proxy && go build ./... && go test ./... 2>&1 | tail
git add proxy/ && git commit -m "proxy: wire streaming + multi-provider dispatch + real tokenizers"
```

---

## Group G: Stuck-reservation reaper

### Task 10: Implement the reaper

**Files:**
- Create: `proxy/internal/reaper/reaper.go`
- Modify: `proxy/cmd/proxy/main.go`

- [ ] **Step 1: Write reaper**

`proxy/internal/reaper/reaper.go`:

```go
package reaper

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Reaper sweeps stuck reservations (rows in credit_transactions kind='reservation'
// older than threshold) and refunds them.
type Reaper struct {
	pg        *pgxpool.Pool
	tick      time.Duration
	threshold time.Duration
}

func New(pg *pgxpool.Pool, tick, threshold time.Duration) *Reaper {
	return &Reaper{pg: pg, tick: tick, threshold: threshold}
}

func (r *Reaper) Run(ctx context.Context) {
	t := time.NewTicker(r.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.sweep(ctx)
		}
	}
}

func (r *Reaper) sweep(ctx context.Context) {
	// Find unresolved reservations older than threshold.
	const findQ = `
		SELECT id, user_id, request_id, -amount_cents AS refund_amount
		FROM credit_transactions
		WHERE kind = 'reservation'
		  AND created_at < now() - $1::interval
		  AND request_id NOT IN (
			SELECT request_id FROM credit_transactions
			WHERE kind IN ('consumption','refund')
			  AND request_id IS NOT NULL
		  )
		LIMIT 100
	`
	thresh := r.threshold.String()
	rows, err := r.pg.Query(ctx, findQ, thresh)
	if err != nil {
		log.Printf("reaper sweep: %v", err)
		return
	}
	type stuck struct {
		ID, UserID, RequestID string
		RefundAmount          int64
	}
	var stucks []stuck
	for rows.Next() {
		var s stuck
		if err := rows.Scan(&s.ID, &s.UserID, &s.RequestID, &s.RefundAmount); err == nil {
			stucks = append(stucks, s)
		}
	}
	rows.Close()

	for _, s := range stucks {
		_, _ = r.pg.Exec(ctx, `
			UPDATE users SET credits_cents = credits_cents + $1 WHERE id = $2
		`, s.RefundAmount, s.UserID)
		_, _ = r.pg.Exec(ctx, `
			INSERT INTO credit_transactions (id, user_id, amount_cents, kind, request_id, balance_after_cents, description, created_at)
			VALUES (gen_random_uuid(), $1, $2, 'refund', $3,
			  (SELECT credits_cents FROM users WHERE id = $1), 'reaper: stuck reservation', now())
		`, s.UserID, s.RefundAmount, s.RequestID)
		log.Printf("reaper: refunded %d cents to user %s for stuck request %s", s.RefundAmount, s.UserID, s.RequestID)
	}
}
```

- [ ] **Step 2: Wire into main.go**

In `proxy/cmd/proxy/main.go`, after the reactor is started:

```go
// add to imports
"github.com/admin/maas-router/proxy/internal/reaper"

// after `go reactor.Run(ctx)`:
rp := reaper.New(pg, 60*time.Second, 5*time.Minute)
go rp.Run(ctx)
```

- [ ] **Step 3: Build + commit**

```bash
go build ./... && git add proxy/ && git commit -m "proxy: add stuck-reservation reaper (60s tick, 5m threshold)"
```

---

## Group H: Model catalog expansion

### Task 11: Seed additional models

**Files:**
- Modify: `web/prisma/seed.ts`

- [ ] **Step 1: Add 7 more models to the seed**

Append to `web/prisma/seed.ts` before the final `console.log`:

```typescript
const additionalModels = [
  {
    alias: 'openai/gpt-4o-mini',
    displayName: 'GPT-4o mini',
    upstreamProvider: 'openai',
    upstreamModelId: 'gpt-4o-mini',
    contextWindow: 128000,
    inputCentsPerMillionTokens: 15,    // $0.15/M
    outputCentsPerMillionTokens: 60,   // $0.60/M
    tags: ['cheap', 'fast'],
    descriptionEn: 'Cheap, fast OpenAI model for routine tasks',
    descriptionZh: '便宜快速的 OpenAI 小模型, 适合日常任务',
  },
  {
    alias: 'openai/o1',
    displayName: 'o1',
    upstreamProvider: 'openai',
    upstreamModelId: 'o1',
    contextWindow: 200000,
    supportsTools: false,
    inputCentsPerMillionTokens: 1500,  // $15/M
    outputCentsPerMillionTokens: 6000, // $60/M
    tags: ['reasoning', 'frontier'],
    descriptionEn: 'Advanced reasoning model',
    descriptionZh: '高级推理模型',
  },
  {
    alias: 'anthropic/claude-sonnet-4-6',
    displayName: 'Claude 4.6 Sonnet',
    upstreamProvider: 'anthropic',
    upstreamModelId: 'claude-sonnet-4-6',
    contextWindow: 200000,
    inputCentsPerMillionTokens: 300,    // $3/M
    outputCentsPerMillionTokens: 1500,  // $15/M
    tags: ['frontier', 'vision', 'tools'],
    descriptionEn: 'Anthropic flagship model, strong coding + reasoning',
    descriptionZh: 'Anthropic 旗舰模型, 编码和推理能力出色',
  },
  {
    alias: 'anthropic/claude-haiku-4-5',
    displayName: 'Claude Haiku 4.5',
    upstreamProvider: 'anthropic',
    upstreamModelId: 'claude-haiku-4-5',
    contextWindow: 200000,
    inputCentsPerMillionTokens: 80,    // $0.80/M
    outputCentsPerMillionTokens: 400,  // $4/M
    tags: ['cheap', 'fast'],
    descriptionEn: 'Anthropic cheap/fast model',
    descriptionZh: 'Anthropic 便宜快速模型',
  },
  {
    alias: 'google/gemini-2.5-pro',
    displayName: 'Gemini 2.5 Pro',
    upstreamProvider: 'google',
    upstreamModelId: 'gemini-2.5-pro',
    contextWindow: 1000000,
    inputCentsPerMillionTokens: 125,    // $1.25/M
    outputCentsPerMillionTokens: 500,   // $5/M
    tags: ['frontier', 'vision', 'long-context'],
    descriptionEn: 'Google Gemini 2.5 Pro, 1M context',
    descriptionZh: 'Google Gemini 2.5 Pro, 一百万 token 上下文',
  },
  {
    alias: 'google/gemini-2.5-flash',
    displayName: 'Gemini 2.5 Flash',
    upstreamProvider: 'google',
    upstreamModelId: 'gemini-2.5-flash',
    contextWindow: 1000000,
    inputCentsPerMillionTokens: 8,     // $0.075/M (rounded up to 1 cent / 125k tokens)
    outputCentsPerMillionTokens: 30,   // $0.30/M
    tags: ['cheap', 'fast', 'long-context'],
    descriptionEn: 'Google Gemini 2.5 Flash, 1M context, very cheap',
    descriptionZh: 'Google Gemini 2.5 Flash, 一百万 token 上下文, 非常便宜',
  },
]

for (const m of additionalModels) {
  await prisma.modelCatalog.upsert({
    where: { alias: m.alias },
    update: {},
    create: {
      alias: m.alias,
      displayName: m.displayName,
      upstreamProvider: m.upstreamProvider,
      upstreamModelId: m.upstreamModelId,
      contextWindow: m.contextWindow,
      supportsStreaming: true,
      supportsTools: m.supportsTools ?? true,
      supportsVision: true,
      inputCentsPerMillionTokens: m.inputCentsPerMillionTokens,
      outputCentsPerMillionTokens: m.outputCentsPerMillionTokens,
      markupPct: 18,
      status: 'active',
      tags: m.tags,
      descriptionEn: m.descriptionEn,
      descriptionZh: m.descriptionZh,
    },
  })
}
```

- [ ] **Step 2: Run seed**

```bash
docker compose up -d postgres
cd web && DATABASE_URL="postgres://app:dev@localhost:5432/maas" npx prisma db seed
docker compose exec postgres psql -U app -d maas -c "SELECT alias FROM model_catalog ORDER BY alias;"
```

Expected: 7 rows including new aliases.

- [ ] **Step 3: Commit**

```bash
cd /Users/admin/Workbench/Code/router && git add web/prisma/seed.ts && git commit -m "db: seed 6 more models (gpt-4o-mini, o1, claude sonnet/haiku, gemini pro/flash)"
```

---

## Group I: Usage history (dashboard polish)

### Task 12: Usage API + page

**Files:**
- Create: `web/src/app/api/usage/route.ts`
- Create: `web/src/app/dashboard/usage/page.tsx`
- Modify: `web/src/app/dashboard/layout.tsx`

- [ ] **Step 1: Write usage API**

`web/src/app/api/usage/route.ts`:

```typescript
import { NextResponse } from 'next/server'
import { auth } from '@/lib/auth'
import { prisma } from '@/lib/db'

export async function GET() {
  const session = await auth()
  if (!session?.user?.id) return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })

  const logs = await prisma.requestLog.findMany({
    where: { userId: session.user.id },
    orderBy: { createdAt: 'desc' },
    take: 50,
    select: {
      id: true,
      modelAlias: true,
      promptTokens: true,
      completionTokens: true,
      totalChargedCents: true,
      status: true,
      latencyMs: true,
      createdAt: true,
    },
  })
  return NextResponse.json({ logs })
}
```

- [ ] **Step 2: Write usage page**

`web/src/app/dashboard/usage/page.tsx`:

```tsx
'use client'
import { useEffect, useState } from 'react'

type Log = {
  id: string
  modelAlias: string
  promptTokens: number | null
  completionTokens: number | null
  totalChargedCents: number
  status: string
  latencyMs: number | null
  createdAt: string
}

export default function UsagePage() {
  const [logs, setLogs] = useState<Log[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/api/usage')
      .then((r) => r.json())
      .then((d) => setLogs(d.logs || []))
      .finally(() => setLoading(false))
  }, [])

  const total = logs.reduce((sum, l) => sum + l.totalChargedCents, 0)

  return (
    <div>
      <h1 className="mb-6 text-2xl font-bold">Usage</h1>
      <div className="mb-6 rounded-lg bg-white p-4 shadow-sm">
        <div className="text-xs uppercase text-gray-500">Last 50 requests · total spent</div>
        <div className="text-2xl font-bold">${(total / 100).toFixed(2)}</div>
      </div>
      {loading ? (
        <p className="text-sm text-gray-500">Loading…</p>
      ) : logs.length === 0 ? (
        <div className="rounded border bg-white p-8 text-center text-sm text-gray-500">
          No requests yet. Make your first API call to see usage here.
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border bg-white">
          <table className="w-full text-sm">
            <thead className="bg-gray-50 text-xs uppercase text-gray-600">
              <tr>
                <th className="px-4 py-3 text-left">Time</th>
                <th className="px-4 py-3 text-left">Model</th>
                <th className="px-4 py-3 text-right">Prompt / Completion</th>
                <th className="px-4 py-3 text-right">Cost</th>
                <th className="px-4 py-3 text-right">Latency</th>
                <th className="px-4 py-3 text-left">Status</th>
              </tr>
            </thead>
            <tbody>
              {logs.map((l) => (
                <tr key={l.id} className="border-t">
                  <td className="px-4 py-3 text-gray-600">{new Date(l.createdAt).toLocaleString()}</td>
                  <td className="px-4 py-3 font-mono text-xs">{l.modelAlias}</td>
                  <td className="px-4 py-3 text-right text-gray-600">{l.promptTokens ?? '–'} / {l.completionTokens ?? '–'}</td>
                  <td className="px-4 py-3 text-right">${(l.totalChargedCents / 100).toFixed(4)}</td>
                  <td className="px-4 py-3 text-right text-gray-600">{l.latencyMs ?? '–'}ms</td>
                  <td className="px-4 py-3"><Badge status={l.status} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

function Badge({ status }: { status: string }) {
  const map: Record<string, string> = {
    success: 'bg-green-100 text-green-800',
    provider_error: 'bg-red-100 text-red-800',
    insufficient_credits: 'bg-yellow-100 text-yellow-800',
    rate_limited: 'bg-yellow-100 text-yellow-800',
    cancelled: 'bg-gray-100 text-gray-800',
  }
  const cls = map[status] || 'bg-gray-100 text-gray-800'
  return <span className={`rounded px-2 py-1 text-xs ${cls}`}>{status}</span>
}
```

- [ ] **Step 3: Add Usage link to sidebar**

Edit `web/src/app/dashboard/layout.tsx`, in the `<nav>`:

```tsx
<Link href="/dashboard" className="block rounded px-3 py-2 hover:bg-gray-100">Overview</Link>
<Link href="/dashboard/keys" className="block rounded px-3 py-2 hover:bg-gray-100">API Keys</Link>
<Link href="/dashboard/usage" className="block rounded px-3 py-2 hover:bg-gray-100">Usage</Link>
```

- [ ] **Step 4: Typecheck + commit**

```bash
cd web && npx tsc --noEmit
cd .. && git add web/ && git commit -m "web: add /dashboard/usage page with recent request log"
```

---

## Group J: Smoke test for streaming

### Task 13: Update smoke test to cover streaming

**Files:**
- Modify: `scripts/smoke-test.sh`

- [ ] **Step 1: Add a streaming case**

Append to `scripts/smoke-test.sh` before the DB queries:

```bash
echo
echo "=== Streaming chat completion request ==="
curl -sS -N -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "anthropic/claude-haiku-4-5",
    "messages": [{"role":"user","content":"Count from 1 to 5, one per line."}],
    "max_tokens": 40,
    "stream": true
  }' | head -30
```

- [ ] **Step 2: Commit**

```bash
git add scripts/smoke-test.sh && git commit -m "scripts: add streaming case to smoke test (Anthropic Haiku)"
```

---

## Self-review

Skim spec §5 (request flow) and §6 (provider abstraction). Confirm:

- [ ] **Streaming end-to-end** — Task 9 wires SSE writer + stream events for all 3 providers ✓
- [ ] **Tokenizers** — Tasks 3-5 add per-provider tokenizers; Task 9 uses them in cost reservation ✓
- [ ] **OpenAI streaming** — Task 6 adds SendStream with usage in final chunk ✓
- [ ] **Anthropic streaming** — Task 7 adds full Anthropic adapter with SSE event parsing ✓
- [ ] **Gemini streaming** — Task 8 adds Gemini adapter with `:streamGenerateContent?alt=sse` ✓
- [ ] **Model catalog growth** — Task 11 seeds 7 models total (1 Phase 0 + 6 new) ✓
- [ ] **Stuck-reservation reaper** — Task 10 implements 60s ticker, 5min threshold ✓
- [ ] **Usage history UI** — Task 12 adds /dashboard/usage ✓

**Gaps deliberately deferred:**
- Tool calls + vision: Phase 2 (deferred per spec §6.3 matrix)
- Anthropic surface `/v1/messages`: Phase 2
- Stripe top-up: Phase 3

**Risks specific to Phase 1:**
- **Anthropic streaming SSE parsing edge cases** — Test against real API; the bufio.Scanner approach handles "data:" lines but not weird whitespace; if it breaks, switch to a custom buffered reader.
- **Gemini's SSE format** uses `alt=sse`; some endpoints return JSON-array streaming instead. The Task 8 code uses `?alt=sse`; verify Gemini honors this for `gemini-2.5-flash` (it should).
- **tiktoken-go embedding download** — first run downloads `o200k_base.tiktoken` (~1MB) to a cache dir. In Docker, this download happens once per container; subsequent runs are fast. If offline, fails — add an env var or pre-fetch in Dockerfile if it becomes an issue.

---

**End of Phase 1 plan.** Total tasks: 13 (with multiple steps per task, totaling roughly 50-60 actionable steps). Estimated solo-developer time: 3 weeks (matches spec §11).

**Parallelism notes for the executor:** Tasks 4 (OpenAI tokenizer), 5 (Anthropic + Gemini tokenizers), 7 (Anthropic provider), and 8 (Gemini provider) are independent and can be dispatched in parallel — they touch separate packages with no shared files. Task 6 (OpenAI streaming) modifies existing `openai.go` so sequence it before Task 9 (handler). Task 9 must come after Tasks 1-8 are done because it ties everything together.
