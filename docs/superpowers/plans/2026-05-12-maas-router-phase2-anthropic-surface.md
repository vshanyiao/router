# MaaS Router — Phase 2: Anthropic Surface + Translation Matrix + Tools + Vision

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax. Tasks within each wave are independent and can be dispatched in parallel.

**Goal:** Add a second API surface at `POST /v1/messages` (Anthropic-compatible), introduce a canonical Internal Representation (IR) that both surfaces lower into, support tool calls and vision input on all three providers, and update the model catalog flags.

**Architecture:** Five layers, matching spec §6.1:
```
Surface handler (OAI or Anthropic)
    ↓ translate
Canonical IR (Request, ContentBlock, Tool, ToolCall, ToolResult, StreamEvent)
    ↓ provider adapter
Provider (OpenAI / Anthropic / Google)
```

For same-format pairs (OAI surface + OAI provider, Anthropic surface + Anthropic provider), keep a fast path that skips canonical conversion to avoid losing data on edge fields we haven't modeled yet (spec §6.2). Cross-format pairs always go through canonical.

**Tech Stack:** No new dependencies. Pure Go on the proxy side. Reuses existing tokenizers + provider HTTP code.

**Reference:** Design spec §6 (Provider Abstraction), §6.3 (Feature Support Matrix).

**Branched from:** `main` (after Phase 0 + Phase 1 + bugfixes merged at `8a30dbd`).

**Out of scope for Phase 2:**
- JSON mode / structured output (deferred to v2 per spec §6.3 matrix red cells)
- Logprobs (deferred — providers other than OpenAI don't expose them)
- Audio in/out (deferred to v3)
- Stripe billing (Phase 3)
- Admin panel (Phase 4)
- Bilingual UI / playground (Phase 5)
- AWS deployment (Phase 6)

---

## File Structure

```
proxy/
  internal/
    canonical/                            + NEW package
      request.go                          IR for input
      content.go                          ContentBlock types (text, image, tool_use, tool_result)
      tool.go                             Tool, ToolCall, ToolChoice
      response.go                         IR for output (non-streaming + stream events)
      content_test.go
      tool_test.go

    translate/                            + NEW package
      oai/                                + NEW
        request.go                        OAI → canonical
        response.go                       canonical → OAI (non-streaming)
        stream.go                         canonical events → OAI SSE chunks
        oai_test.go
      anthropic/                          + NEW
        request.go                        Anthropic → canonical
        response.go                       canonical → Anthropic (non-streaming)
        stream.go                         canonical events → Anthropic SSE
        anthropic_test.go

    provider/
      provider.go                         ⚠ EXTEND: richer Request, StreamEvent (existing fields kept)
      openai/openai.go                    ⚠ MODIFIED: use canonical types; support tools + vision
      anthropic/anthropic.go              ⚠ MODIFIED: same
      gemini/gemini.go                    ⚠ MODIFIED: same

    server/
      openai.go                           ⚠ MODIFIED: delegate to translate/oai
      anthropic.go                        + NEW: /v1/messages handler delegating to translate/anthropic
      anthropic_test.go

  cmd/proxy/main.go                       ⚠ MODIFIED: register /v1/messages route

web/
  prisma/seed.ts                          ⚠ MODIFIED: set supports_tools=true / supports_vision=true correctly per model
```

Roughly +1200 lines of new code, ~300 lines modified.

---

## Group A: Canonical IR (foundation — sequential)

### Task 1: Canonical content blocks

**Files:**
- Create: `proxy/internal/canonical/content.go`
- Create: `proxy/internal/canonical/content_test.go`

- [ ] **Step 1: Write failing test**

`proxy/internal/canonical/content_test.go`:

```go
package canonical

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContentBlock_Text(t *testing.T) {
	b := TextBlock("hello")
	assert.Equal(t, BlockText, b.Type)
	assert.Equal(t, "hello", b.Text)
}

func TestContentBlock_Image(t *testing.T) {
	b := ImageBlock("https://example.com/foo.png", "")
	assert.Equal(t, BlockImage, b.Type)
	assert.Equal(t, "https://example.com/foo.png", b.ImageURL)
	assert.Empty(t, b.ImageData)
}

func TestContentBlock_ToolUse(t *testing.T) {
	b := ToolUseBlock("call_abc", "get_weather", []byte(`{"city":"Paris"}`))
	assert.Equal(t, BlockToolUse, b.Type)
	assert.Equal(t, "call_abc", b.ToolUseID)
	assert.Equal(t, "get_weather", b.ToolName)
	assert.JSONEq(t, `{"city":"Paris"}`, string(b.ToolInput))
}

func TestContentBlock_ToolResult(t *testing.T) {
	b := ToolResultBlock("call_abc", "Sunny, 22°C")
	assert.Equal(t, BlockToolResult, b.Type)
	assert.Equal(t, "call_abc", b.ToolUseID)
	assert.Equal(t, "Sunny, 22°C", b.ToolResult)
}
```

- [ ] **Step 2: Implement**

`proxy/internal/canonical/content.go`:

```go
// Package canonical defines the provider-neutral IR used between API surface
// handlers and provider adapters. Both inbound surfaces (OAI /v1/chat/completions
// and Anthropic /v1/messages) lower into these types; each provider adapter
// reads them and emits its own wire format.
package canonical

import "encoding/json"

// BlockType enumerates the supported content block kinds.
type BlockType string

const (
	BlockText       BlockType = "text"
	BlockImage      BlockType = "image"
	BlockToolUse    BlockType = "tool_use"
	BlockToolResult BlockType = "tool_result"
)

// ContentBlock is the unit of multi-modal content. A message's Content is
// always a slice of these, even for plain text (a string at the wire format
// gets wrapped in a single TextBlock at the surface boundary).
type ContentBlock struct {
	Type BlockType

	// Text: present when Type == BlockText.
	Text string

	// Image: present when Type == BlockImage. Exactly one of ImageURL or
	// ImageData is set. MediaType is the MIME type (e.g. "image/png");
	// required when ImageData is set.
	ImageURL  string
	ImageData []byte
	MediaType string

	// ToolUse: present when Type == BlockToolUse. The assistant is calling a
	// tool. ToolInput is the raw JSON of the arguments.
	ToolUseID string
	ToolName  string
	ToolInput json.RawMessage

	// ToolResult: present when Type == BlockToolResult. The user is supplying
	// the result of a previous tool call. ToolUseID matches the assistant's
	// ToolUseID. ToolResult is the textual result (we don't model multi-modal
	// tool results in Phase 2; defer).
	ToolResult string
}

func TextBlock(text string) ContentBlock {
	return ContentBlock{Type: BlockText, Text: text}
}

func ImageBlock(url string, mediaType string) ContentBlock {
	return ContentBlock{Type: BlockImage, ImageURL: url, MediaType: mediaType}
}

func ImageDataBlock(data []byte, mediaType string) ContentBlock {
	return ContentBlock{Type: BlockImage, ImageData: data, MediaType: mediaType}
}

func ToolUseBlock(id, name string, input json.RawMessage) ContentBlock {
	return ContentBlock{Type: BlockToolUse, ToolUseID: id, ToolName: name, ToolInput: input}
}

func ToolResultBlock(toolUseID, result string) ContentBlock {
	return ContentBlock{Type: BlockToolResult, ToolUseID: toolUseID, ToolResult: result}
}
```

- [ ] **Step 3: Run test, commit**

```bash
cd /Users/admin/Workbench/Code/router/proxy && go test ./internal/canonical/...
git add proxy/internal/canonical/ && git commit -m "canonical: add ContentBlock with text/image/tool_use/tool_result variants"
```

---

### Task 2: Canonical tools

**Files:**
- Create: `proxy/internal/canonical/tool.go`
- Create: `proxy/internal/canonical/tool_test.go`

- [ ] **Step 1: Write tool types**

`proxy/internal/canonical/tool.go`:

```go
package canonical

import "encoding/json"

// Tool is a function the model can call. Schema follows JSON Schema and is
// passed through unchanged to providers.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ToolChoiceMode controls whether/which tool the model should call.
type ToolChoiceMode string

const (
	ToolChoiceAuto     ToolChoiceMode = "auto"     // model decides (default)
	ToolChoiceNone     ToolChoiceMode = "none"     // model must respond directly
	ToolChoiceAny      ToolChoiceMode = "any"      // model must call some tool
	ToolChoiceSpecific ToolChoiceMode = "specific" // model must call ToolName
)

type ToolChoice struct {
	Mode     ToolChoiceMode
	ToolName string // only when Mode == ToolChoiceSpecific
}
```

- [ ] **Step 2: Test (constructor smoke)**

`proxy/internal/canonical/tool_test.go`:

```go
package canonical

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTool_DefaultToolChoice(t *testing.T) {
	tc := ToolChoice{}
	assert.Empty(t, string(tc.Mode), "zero-value mode is empty; handlers should treat as auto")
}

func TestToolChoice_Specific(t *testing.T) {
	tc := ToolChoice{Mode: ToolChoiceSpecific, ToolName: "get_weather"}
	assert.Equal(t, "get_weather", tc.ToolName)
}
```

- [ ] **Step 3: Commit**

```bash
go test ./internal/canonical/...
git add proxy/internal/canonical/ && git commit -m "canonical: add Tool and ToolChoice"
```

---

### Task 3: Canonical Request, Response, StreamEvent

**Files:**
- Create: `proxy/internal/canonical/request.go`
- Create: `proxy/internal/canonical/response.go`

- [ ] **Step 1: Define request type**

`proxy/internal/canonical/request.go`:

```go
package canonical

// Role enumerates message roles. System is separate from Messages (see Request).
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool" // OAI-style tool response messages; both Anthropic
	                            // and Gemini fold these into user messages with
	                            // tool_result content blocks during lowering.
)

// Message is one turn in the conversation. Content is always a slice (a wire-
// format string becomes []ContentBlock{TextBlock(s)} at the surface boundary).
type Message struct {
	Role    Role
	Content []ContentBlock
}

// Request is the canonical inference request. Surface handlers lower into this;
// provider adapters consume it. Note: System is separate so providers that have
// a dedicated system field (Anthropic, Gemini) can populate it directly without
// hunting through Messages.
type Request struct {
	Model         string   // upstream model id (the provider-native name)
	System        string   // empty if not provided
	Messages      []Message

	MaxTokens     int
	Temperature   *float64
	TopP          *float64
	StopSequences []string

	Tools         []Tool
	ToolChoice    ToolChoice

	Stream        bool
}
```

- [ ] **Step 2: Define response + stream event types**

`proxy/internal/canonical/response.go`:

```go
package canonical

// Response is a complete (non-streaming) inference result. The assistant's
// turn is represented as a slice of content blocks (text + zero or more
// tool_use blocks).
type Response struct {
	Content    []ContentBlock
	StopReason string // "stop" | "length" | "tool_use" | "cancelled"
	Usage      Usage
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

// StreamEventType enumerates the events a streaming response can produce.
// Each surface translator maps these to its own wire format.
type StreamEventType string

const (
	StreamEventContent       StreamEventType = "content_delta"
	StreamEventToolCallStart StreamEventType = "tool_call_start"
	StreamEventToolCallDelta StreamEventType = "tool_call_delta"
	StreamEventToolCallStop  StreamEventType = "tool_call_stop"
	StreamEventUsage         StreamEventType = "usage"
	StreamEventStop          StreamEventType = "stop"
	StreamEventError         StreamEventType = "error"
)

// StreamEvent is one event from the canonical stream.
type StreamEvent struct {
	Type StreamEventType

	// Content delta: text added to the assistant's reply.
	ContentDelta string

	// Tool call lifecycle: start defines (id, name), delta appends to args,
	// stop signals the call args are complete.
	ToolCallID         string
	ToolCallName       string
	ToolCallArgsDelta  string

	// Usage: prompt/completion tokens. Emitted at most once, near the end.
	Usage *Usage

	// Stop: reason the stream ended.
	StopReason string

	// Error: present when Type == StreamEventError.
	Error *ErrorInfo
}

type ErrorInfo struct {
	Code    string
	Message string
}
```

- [ ] **Step 3: Build and commit**

```bash
go build ./internal/canonical/...
git add proxy/internal/canonical/ && git commit -m "canonical: add Request, Response, StreamEvent for the IR"
```

---

## Group B: OAI surface translators (independent — parallel with Group C)

### Task 4: OAI request → canonical

**Files:**
- Create: `proxy/internal/translate/oai/request.go`
- Create: `proxy/internal/translate/oai/request_test.go`

- [ ] **Step 1: Define OAI wire types (re-use server/openai.go's request shape but extended)**

Move/extend the existing wire types. For Phase 2 they live in the translator package so each surface owns its types.

`proxy/internal/translate/oai/request.go`:

```go
// Package oai handles translation between the OpenAI-compatible API surface
// (POST /v1/chat/completions wire format) and the canonical IR.
package oai

import (
	"encoding/json"
	"fmt"

	"github.com/admin/maas-router/proxy/internal/canonical"
)

// ChatCompletionsRequest matches OpenAI's wire format. Use json.RawMessage for
// the parts of the protocol we want to forward without parsing.
type ChatCompletionsRequest struct {
	Model       string                     `json:"model"`
	Messages    []ChatMessage              `json:"messages"`
	MaxTokens   int                        `json:"max_tokens,omitempty"`
	Temperature *float64                   `json:"temperature,omitempty"`
	TopP        *float64                   `json:"top_p,omitempty"`
	Stop        StopField                  `json:"stop,omitempty"`
	Stream      bool                       `json:"stream,omitempty"`
	Tools       []ToolDef                  `json:"tools,omitempty"`
	ToolChoice  json.RawMessage            `json:"tool_choice,omitempty"`
}

// StopField is OAI's "stop" — accepts a single string or a list of strings.
type StopField []string

func (s *StopField) UnmarshalJSON(data []byte) error {
	// Try array first
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*s = arr
		return nil
	}
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*s = []string{single}
		return nil
	}
	return fmt.Errorf("stop must be a string or array of strings")
}

// ChatMessage's Content is either a string or an array of content parts. We
// keep RawContent as RawMessage and normalize in the translator.
type ChatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  []ChatToolCall  `json:"tool_calls,omitempty"`  // assistant role only
	ToolCallID string          `json:"tool_call_id,omitempty"` // tool role only
}

// OAI content parts. When Content is an array, each part has a "type" of
// "text" or "image_url".
type ContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *ContentImageURL `json:"image_url,omitempty"`
}

type ContentImageURL struct {
	URL string `json:"url"`
}

type ChatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // always "function" for now
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON string, not nested object
	} `json:"function"`
}

type ToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

// ToCanonical converts an OAI request into the canonical IR. Returns an error
// if the input is malformed (e.g. unknown content part type).
func ToCanonical(req ChatCompletionsRequest) (canonical.Request, error) {
	out := canonical.Request{
		Model:         req.Model,
		MaxTokens:     req.MaxTokens,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		StopSequences: []string(req.Stop),
		Stream:        req.Stream,
	}

	// Lower tools
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, canonical.Tool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}

	// Lower tool_choice (OAI: "auto" | "none" | "required" | {type:"function",function:{name}})
	out.ToolChoice = parseToolChoice(req.ToolChoice)

	// Lower messages — extract system, normalize content
	for _, m := range req.Messages {
		blocks, err := parseContent(m.Content)
		if err != nil {
			return canonical.Request{}, fmt.Errorf("message[%s] content: %w", m.Role, err)
		}
		switch m.Role {
		case "system":
			if out.System != "" {
				out.System += "\n\n"
			}
			out.System += contentToText(blocks)
		case "tool":
			// OAI tool response — fold into a user message with a tool_result block
			out.Messages = append(out.Messages, canonical.Message{
				Role: canonical.RoleUser,
				Content: []canonical.ContentBlock{
					canonical.ToolResultBlock(m.ToolCallID, contentToText(blocks)),
				},
			})
		case "assistant":
			// Assistant: may have text + tool_calls
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, canonical.ToolUseBlock(tc.ID, tc.Function.Name, json.RawMessage(tc.Function.Arguments)))
			}
			out.Messages = append(out.Messages, canonical.Message{
				Role:    canonical.RoleAssistant,
				Content: blocks,
			})
		case "user":
			out.Messages = append(out.Messages, canonical.Message{
				Role:    canonical.RoleUser,
				Content: blocks,
			})
		default:
			return canonical.Request{}, fmt.Errorf("unsupported role %q", m.Role)
		}
	}
	return out, nil
}

// parseContent handles OAI's polymorphic "content" field — string or array.
func parseContent(raw json.RawMessage) ([]canonical.ContentBlock, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	// Try string first
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []canonical.ContentBlock{canonical.TextBlock(s)}, nil
	}
	// Array of parts
	var parts []ContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("content must be string or array: %w", err)
	}
	out := make([]canonical.ContentBlock, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case "text":
			out = append(out, canonical.TextBlock(p.Text))
		case "image_url":
			if p.ImageURL == nil {
				return nil, fmt.Errorf("image_url part missing image_url object")
			}
			out = append(out, canonical.ImageBlock(p.ImageURL.URL, ""))
		default:
			return nil, fmt.Errorf("unsupported content part type %q", p.Type)
		}
	}
	return out, nil
}

func contentToText(blocks []canonical.ContentBlock) string {
	var s string
	for _, b := range blocks {
		if b.Type == canonical.BlockText {
			s += b.Text
		}
	}
	return s
}

func parseToolChoice(raw json.RawMessage) canonical.ToolChoice {
	if len(raw) == 0 {
		return canonical.ToolChoice{Mode: canonical.ToolChoiceAuto}
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch s {
		case "none":
			return canonical.ToolChoice{Mode: canonical.ToolChoiceNone}
		case "required":
			return canonical.ToolChoice{Mode: canonical.ToolChoiceAny}
		default:
			return canonical.ToolChoice{Mode: canonical.ToolChoiceAuto}
		}
	}
	var obj struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Function.Name != "" {
		return canonical.ToolChoice{Mode: canonical.ToolChoiceSpecific, ToolName: obj.Function.Name}
	}
	return canonical.ToolChoice{Mode: canonical.ToolChoiceAuto}
}
```

- [ ] **Step 2: Tests**

`proxy/internal/translate/oai/request_test.go`:

```go
package oai

import (
	"encoding/json"
	"testing"

	"github.com/admin/maas-router/proxy/internal/canonical"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToCanonical_StringContent(t *testing.T) {
	req := ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	}
	got, err := ToCanonical(req)
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	require.Len(t, got.Messages[0].Content, 1)
	assert.Equal(t, canonical.BlockText, got.Messages[0].Content[0].Type)
	assert.Equal(t, "hello", got.Messages[0].Content[0].Text)
}

func TestToCanonical_SystemExtracted(t *testing.T) {
	req := ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "system", Content: json.RawMessage(`"be helpful"`)},
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
	}
	got, err := ToCanonical(req)
	require.NoError(t, err)
	assert.Equal(t, "be helpful", got.System)
	require.Len(t, got.Messages, 1, "system should be extracted, not in messages")
}

func TestToCanonical_ImageContent(t *testing.T) {
	req := ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`[
				{"type":"text","text":"what's in this?"},
				{"type":"image_url","image_url":{"url":"https://x.com/cat.png"}}
			]`)},
		},
	}
	got, err := ToCanonical(req)
	require.NoError(t, err)
	require.Len(t, got.Messages[0].Content, 2)
	assert.Equal(t, canonical.BlockText, got.Messages[0].Content[0].Type)
	assert.Equal(t, canonical.BlockImage, got.Messages[0].Content[1].Type)
	assert.Equal(t, "https://x.com/cat.png", got.Messages[0].Content[1].ImageURL)
}

func TestToCanonical_ToolCallsAndResponses(t *testing.T) {
	req := ChatCompletionsRequest{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"weather in Paris?"`)},
			{
				Role:    "assistant",
				Content: json.RawMessage(`null`),
				ToolCalls: []ChatToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "get_weather", Arguments: `{"city":"Paris"}`},
				}},
			},
			{Role: "tool", ToolCallID: "call_1", Content: json.RawMessage(`"sunny"`)},
		},
	}
	got, err := ToCanonical(req)
	require.NoError(t, err)
	require.Len(t, got.Messages, 3)
	// Second message: assistant with one tool_use block
	assert.Equal(t, canonical.RoleAssistant, got.Messages[1].Role)
	require.Len(t, got.Messages[1].Content, 1)
	assert.Equal(t, canonical.BlockToolUse, got.Messages[1].Content[0].Type)
	assert.Equal(t, "call_1", got.Messages[1].Content[0].ToolUseID)
	assert.Equal(t, "get_weather", got.Messages[1].Content[0].ToolName)
	// Third message: tool response folded into user role with tool_result
	assert.Equal(t, canonical.RoleUser, got.Messages[2].Role)
	require.Len(t, got.Messages[2].Content, 1)
	assert.Equal(t, canonical.BlockToolResult, got.Messages[2].Content[0].Type)
	assert.Equal(t, "call_1", got.Messages[2].Content[0].ToolUseID)
	assert.Equal(t, "sunny", got.Messages[2].Content[0].ToolResult)
}

func TestToCanonical_ToolDefs(t *testing.T) {
	req := ChatCompletionsRequest{
		Model: "gpt-4o",
		Tools: []ToolDef{{
			Type: "function",
			Function: struct {
				Name        string          `json:"name"`
				Description string          `json:"description,omitempty"`
				Parameters  json.RawMessage `json:"parameters"`
			}{
				Name:        "get_weather",
				Description: "get current weather",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			},
		}},
	}
	got, err := ToCanonical(req)
	require.NoError(t, err)
	require.Len(t, got.Tools, 1)
	assert.Equal(t, "get_weather", got.Tools[0].Name)
	assert.JSONEq(t, `{"type":"object"}`, string(got.Tools[0].InputSchema))
}
```

- [ ] **Step 3: Run tests, commit**

```bash
go test ./internal/translate/oai/...
git add proxy/internal/translate/oai/ && git commit -m "translate/oai: lower OAI requests (incl. tools + vision) to canonical IR"
```

---

### Task 5: canonical → OAI response (non-streaming)

**Files:**
- Create: `proxy/internal/translate/oai/response.go`

- [ ] **Step 1: Write the FromCanonical function**

```go
package oai

import "github.com/admin/maas-router/proxy/internal/canonical"

// ChatCompletionsResponse mirrors OpenAI's response wire format.
type ChatCompletionsResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []Choice       `json:"choices"`
	Usage   UsagePayload   `json:"usage"`
}

type Choice struct {
	Index        int           `json:"index"`
	Message      RespMessage   `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type RespMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	ToolCalls []ChatToolCall `json:"tool_calls,omitempty"`
}

type UsagePayload struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// FromCanonical builds an OAI response from a canonical Response. The
// caller supplies id, model alias, and created timestamp.
func FromCanonical(id, modelAlias string, created int64, r canonical.Response) ChatCompletionsResponse {
	var text string
	var toolCalls []ChatToolCall
	for _, b := range r.Content {
		switch b.Type {
		case canonical.BlockText:
			text += b.Text
		case canonical.BlockToolUse:
			tc := ChatToolCall{ID: b.ToolUseID, Type: "function"}
			tc.Function.Name = b.ToolName
			tc.Function.Arguments = string(b.ToolInput)
			toolCalls = append(toolCalls, tc)
		}
	}
	finish := mapStopReason(r.StopReason)
	return ChatCompletionsResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   modelAlias,
		Choices: []Choice{{
			Index:        0,
			Message:      RespMessage{Role: "assistant", Content: text, ToolCalls: toolCalls},
			FinishReason: finish,
		}},
		Usage: UsagePayload{
			PromptTokens:     r.Usage.PromptTokens,
			CompletionTokens: r.Usage.CompletionTokens,
			TotalTokens:      r.Usage.PromptTokens + r.Usage.CompletionTokens,
		},
	}
}

func mapStopReason(r string) string {
	switch r {
	case "tool_use":
		return "tool_calls"
	case "length":
		return "length"
	case "cancelled":
		return "cancelled"
	default:
		return "stop"
	}
}
```

- [ ] **Step 2: Commit**

```bash
go build ./internal/translate/oai/...
git add proxy/internal/translate/oai/response.go
git commit -m "translate/oai: build OAI ChatCompletionsResponse from canonical (incl. tool_calls)"
```

---

### Task 6: canonical events → OAI SSE chunks

**Files:**
- Create: `proxy/internal/translate/oai/stream.go`

- [ ] **Step 1: Implement**

```go
package oai

import "github.com/admin/maas-router/proxy/internal/canonical"

// StreamingChunk is one SSE event in OAI's stream wire format.
type StreamingChunk struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []StreamingChoice  `json:"choices"`
	Usage   *UsagePayload      `json:"usage,omitempty"`
}

type StreamingChoice struct {
	Index        int                `json:"index"`
	Delta        StreamingDelta     `json:"delta"`
	FinishReason *string            `json:"finish_reason,omitempty"`
}

type StreamingDelta struct {
	Role      string                 `json:"role,omitempty"`
	Content   string                 `json:"content,omitempty"`
	ToolCalls []StreamingToolCall    `json:"tool_calls,omitempty"`
}

type StreamingToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function,omitempty"`
}

// StreamMapper translates canonical stream events into OAI SSE chunks. It is
// stateful because tool_call streams need to remember the index they were
// assigned (OAI numbers tool calls within a single response).
type StreamMapper struct {
	ID         string
	Model      string
	Created    int64
	toolIndex  map[string]int // canonical ToolCallID → OAI tool_calls index
	nextIndex  int
}

func NewStreamMapper(id, modelAlias string, created int64) *StreamMapper {
	return &StreamMapper{ID: id, Model: modelAlias, Created: created, toolIndex: map[string]int{}}
}

// Map produces zero or one StreamingChunk for a canonical event.
// Returns (nil, nil) for events that don't translate to a chunk.
func (m *StreamMapper) Map(evt canonical.StreamEvent) *StreamingChunk {
	switch evt.Type {
	case canonical.StreamEventContent:
		if evt.ContentDelta == "" {
			return nil
		}
		return &StreamingChunk{
			ID: m.ID, Object: "chat.completion.chunk", Created: m.Created, Model: m.Model,
			Choices: []StreamingChoice{{Index: 0, Delta: StreamingDelta{Content: evt.ContentDelta}}},
		}
	case canonical.StreamEventToolCallStart:
		idx := m.nextIndex
		m.toolIndex[evt.ToolCallID] = idx
		m.nextIndex++
		tc := StreamingToolCall{Index: idx, ID: evt.ToolCallID, Type: "function"}
		tc.Function.Name = evt.ToolCallName
		return &StreamingChunk{
			ID: m.ID, Object: "chat.completion.chunk", Created: m.Created, Model: m.Model,
			Choices: []StreamingChoice{{Index: 0, Delta: StreamingDelta{ToolCalls: []StreamingToolCall{tc}}}},
		}
	case canonical.StreamEventToolCallDelta:
		idx, ok := m.toolIndex[evt.ToolCallID]
		if !ok {
			return nil
		}
		tc := StreamingToolCall{Index: idx}
		tc.Function.Arguments = evt.ToolCallArgsDelta
		return &StreamingChunk{
			ID: m.ID, Object: "chat.completion.chunk", Created: m.Created, Model: m.Model,
			Choices: []StreamingChoice{{Index: 0, Delta: StreamingDelta{ToolCalls: []StreamingToolCall{tc}}}},
		}
	case canonical.StreamEventStop:
		fr := mapStopReason(evt.StopReason)
		return &StreamingChunk{
			ID: m.ID, Object: "chat.completion.chunk", Created: m.Created, Model: m.Model,
			Choices: []StreamingChoice{{Index: 0, Delta: StreamingDelta{}, FinishReason: &fr}},
		}
	}
	return nil
}
```

- [ ] **Step 2: Build and commit**

```bash
go build ./internal/translate/oai/...
git add proxy/internal/translate/oai/stream.go
git commit -m "translate/oai: map canonical stream events to OAI SSE chunks (incl. streaming tool_calls)"
```

---

## Group C: Anthropic surface translators (independent — parallel with Group B)

### Task 7: Anthropic request → canonical

**Files:**
- Create: `proxy/internal/translate/anthropic/request.go`
- Create: `proxy/internal/translate/anthropic/request_test.go`

- [ ] **Step 1: Write wire types + ToCanonical**

```go
// Package anthropic handles translation between Anthropic's /v1/messages
// surface wire format and the canonical IR.
package anthropic

import (
	"encoding/json"
	"fmt"

	"github.com/admin/maas-router/proxy/internal/canonical"
)

type MessagesRequest struct {
	Model         string          `json:"model"`
	System        json.RawMessage `json:"system,omitempty"` // string or array of {type:text,text}
	Messages      []WireMessage   `json:"messages"`
	MaxTokens     int             `json:"max_tokens"`
	Temperature   *float64        `json:"temperature,omitempty"`
	TopP          *float64        `json:"top_p,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	Tools         []ToolWire      `json:"tools,omitempty"`
	ToolChoice    json.RawMessage `json:"tool_choice,omitempty"`
}

type WireMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type ContentBlockWire struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Source    *ImageSource    `json:"source,omitempty"`
	ID        string          `json:"id,omitempty"`         // tool_use
	Name      string          `json:"name,omitempty"`       // tool_use
	Input     json.RawMessage `json:"input,omitempty"`      // tool_use
	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result
	Content   json.RawMessage `json:"content,omitempty"`    // tool_result (string or array)
}

type ImageSource struct {
	Type      string `json:"type"`       // "url" or "base64"
	URL       string `json:"url,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"` // base64 when Type=="base64"
}

type ToolWire struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ToCanonical converts an Anthropic /v1/messages request into canonical IR.
func ToCanonical(req MessagesRequest) (canonical.Request, error) {
	out := canonical.Request{
		Model:         req.Model,
		MaxTokens:     req.MaxTokens,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		StopSequences: req.StopSequences,
		Stream:        req.Stream,
	}

	// System — Anthropic accepts string or array of text blocks
	if len(req.System) > 0 {
		var s string
		if err := json.Unmarshal(req.System, &s); err == nil {
			out.System = s
		} else {
			var blocks []ContentBlockWire
			if err := json.Unmarshal(req.System, &blocks); err != nil {
				return canonical.Request{}, fmt.Errorf("system must be string or array: %w", err)
			}
			for _, b := range blocks {
				if b.Type == "text" {
					out.System += b.Text
				}
			}
		}
	}

	// Tools
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, canonical.Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}

	out.ToolChoice = parseToolChoice(req.ToolChoice)

	// Messages
	for i, m := range req.Messages {
		blocks, err := parseContent(m.Content)
		if err != nil {
			return canonical.Request{}, fmt.Errorf("messages[%d]: %w", i, err)
		}
		role := canonical.RoleUser
		if m.Role == "assistant" {
			role = canonical.RoleAssistant
		}
		out.Messages = append(out.Messages, canonical.Message{Role: role, Content: blocks})
	}
	return out, nil
}

func parseContent(raw json.RawMessage) ([]canonical.ContentBlock, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	// String shortcut: equivalent to one text block
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []canonical.ContentBlock{canonical.TextBlock(s)}, nil
	}
	var parts []ContentBlockWire
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("content must be string or array of blocks: %w", err)
	}
	out := make([]canonical.ContentBlock, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case "text":
			out = append(out, canonical.TextBlock(p.Text))
		case "image":
			if p.Source == nil {
				return nil, fmt.Errorf("image block missing source")
			}
			if p.Source.Type == "url" {
				out = append(out, canonical.ImageBlock(p.Source.URL, p.Source.MediaType))
			} else if p.Source.Type == "base64" {
				out = append(out, canonical.ImageDataBlock([]byte(p.Source.Data), p.Source.MediaType))
			}
		case "tool_use":
			out = append(out, canonical.ToolUseBlock(p.ID, p.Name, p.Input))
		case "tool_result":
			// Anthropic tool_result can carry a string or an array — flatten to string
			var resText string
			if err := json.Unmarshal(p.Content, &resText); err != nil {
				var sub []ContentBlockWire
				if err := json.Unmarshal(p.Content, &sub); err == nil {
					for _, s := range sub {
						if s.Type == "text" { resText += s.Text }
					}
				}
			}
			out = append(out, canonical.ToolResultBlock(p.ToolUseID, resText))
		default:
			return nil, fmt.Errorf("unsupported content block type %q", p.Type)
		}
	}
	return out, nil
}

func parseToolChoice(raw json.RawMessage) canonical.ToolChoice {
	if len(raw) == 0 {
		return canonical.ToolChoice{Mode: canonical.ToolChoiceAuto}
	}
	var obj struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		switch obj.Type {
		case "any":
			return canonical.ToolChoice{Mode: canonical.ToolChoiceAny}
		case "tool":
			return canonical.ToolChoice{Mode: canonical.ToolChoiceSpecific, ToolName: obj.Name}
		case "auto":
			return canonical.ToolChoice{Mode: canonical.ToolChoiceAuto}
		}
	}
	return canonical.ToolChoice{Mode: canonical.ToolChoiceAuto}
}
```

- [ ] **Step 2: Tests for the key paths**

`proxy/internal/translate/anthropic/request_test.go`:

```go
package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/admin/maas-router/proxy/internal/canonical"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToCanonical_System_Plain(t *testing.T) {
	req := MessagesRequest{
		Model:  "claude-sonnet-4-6",
		System: json.RawMessage(`"be helpful"`),
		Messages: []WireMessage{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
		MaxTokens: 100,
	}
	got, err := ToCanonical(req)
	require.NoError(t, err)
	assert.Equal(t, "be helpful", got.System)
}

func TestToCanonical_ImageAndToolUse(t *testing.T) {
	req := MessagesRequest{
		Model: "claude-sonnet-4-6",
		Messages: []WireMessage{
			{Role: "user", Content: json.RawMessage(`[
				{"type":"text","text":"what's this?"},
				{"type":"image","source":{"type":"url","url":"https://x.com/cat.png","media_type":"image/png"}}
			]`)},
			{Role: "assistant", Content: json.RawMessage(`[
				{"type":"text","text":"checking..."},
				{"type":"tool_use","id":"toolu_1","name":"vision","input":{"region":"main"}}
			]`)},
		},
		MaxTokens: 100,
	}
	got, err := ToCanonical(req)
	require.NoError(t, err)
	require.Len(t, got.Messages, 2)
	// User message has text + image
	require.Len(t, got.Messages[0].Content, 2)
	assert.Equal(t, canonical.BlockImage, got.Messages[0].Content[1].Type)
	assert.Equal(t, "https://x.com/cat.png", got.Messages[0].Content[1].ImageURL)
	// Assistant has text + tool_use
	require.Len(t, got.Messages[1].Content, 2)
	assert.Equal(t, canonical.BlockToolUse, got.Messages[1].Content[1].Type)
	assert.Equal(t, "toolu_1", got.Messages[1].Content[1].ToolUseID)
	assert.Equal(t, "vision", got.Messages[1].Content[1].ToolName)
}

func TestToCanonical_ToolResult(t *testing.T) {
	req := MessagesRequest{
		Model: "claude-sonnet-4-6",
		Messages: []WireMessage{
			{Role: "user", Content: json.RawMessage(`[
				{"type":"tool_result","tool_use_id":"toolu_1","content":"cat detected"}
			]`)},
		},
		MaxTokens: 100,
	}
	got, err := ToCanonical(req)
	require.NoError(t, err)
	require.Len(t, got.Messages[0].Content, 1)
	assert.Equal(t, canonical.BlockToolResult, got.Messages[0].Content[0].Type)
	assert.Equal(t, "toolu_1", got.Messages[0].Content[0].ToolUseID)
	assert.Equal(t, "cat detected", got.Messages[0].Content[0].ToolResult)
}
```

- [ ] **Step 3: Run + commit**

```bash
go test ./internal/translate/anthropic/...
git add proxy/internal/translate/anthropic/ && git commit -m "translate/anthropic: lower Anthropic /v1/messages requests to canonical"
```

---

### Task 8: canonical → Anthropic response

**Files:**
- Create: `proxy/internal/translate/anthropic/response.go`

- [ ] **Step 1: Implement**

```go
package anthropic

import "github.com/admin/maas-router/proxy/internal/canonical"

// MessagesResponse mirrors Anthropic's /v1/messages response.
type MessagesResponse struct {
	ID         string             `json:"id"`
	Type       string             `json:"type"` // "message"
	Role       string             `json:"role"` // "assistant"
	Model      string             `json:"model"`
	Content    []ContentBlockWire `json:"content"`
	StopReason string             `json:"stop_reason"`
	Usage      UsageWire          `json:"usage"`
}

type UsageWire struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func FromCanonical(id, modelAlias string, r canonical.Response) MessagesResponse {
	content := make([]ContentBlockWire, 0, len(r.Content))
	for _, b := range r.Content {
		switch b.Type {
		case canonical.BlockText:
			content = append(content, ContentBlockWire{Type: "text", Text: b.Text})
		case canonical.BlockToolUse:
			content = append(content, ContentBlockWire{
				Type: "tool_use", ID: b.ToolUseID, Name: b.ToolName, Input: b.ToolInput,
			})
		}
	}
	return MessagesResponse{
		ID:         id,
		Type:       "message",
		Role:       "assistant",
		Model:      modelAlias,
		Content:    content,
		StopReason: mapStopReason(r.StopReason),
		Usage:      UsageWire{InputTokens: r.Usage.PromptTokens, OutputTokens: r.Usage.CompletionTokens},
	}
}

func mapStopReason(r string) string {
	switch r {
	case "tool_use":
		return "tool_use"
	case "length":
		return "max_tokens"
	case "cancelled":
		return "stop_sequence"
	default:
		return "end_turn"
	}
}
```

- [ ] **Step 2: Commit**

```bash
go build ./internal/translate/anthropic/...
git add proxy/internal/translate/anthropic/response.go
git commit -m "translate/anthropic: build /v1/messages response from canonical"
```

---

### Task 9: canonical events → Anthropic SSE

**Files:**
- Create: `proxy/internal/translate/anthropic/stream.go`

- [ ] **Step 1: Implement Anthropic typed-event stream mapper**

Anthropic's SSE format uses named events (not just `data:` lines). Each event has an `event:` header followed by a `data:` JSON line. Sequence:

```
event: message_start  → message_start with id, model, role, empty content, input usage
event: content_block_start (index 0)  → first text block
event: content_block_delta (index 0)  → text_delta for each chunk
event: content_block_stop  (index 0)
event: content_block_start (index 1, tool_use)
event: content_block_delta (index 1, input_json_delta)
event: content_block_stop  (index 1)
event: message_delta  → stop_reason + output usage delta
event: message_stop
```

```go
package anthropic

import (
	"encoding/json"
	"fmt"

	"github.com/admin/maas-router/proxy/internal/canonical"
)

// FrameEvent represents one named-event SSE frame to write to the wire.
type FrameEvent struct {
	Event string
	Data  string // marshaled JSON
}

// StreamMapper maintains state across a single Anthropic-format stream so
// content_block_start / _delta / _stop events stay paired correctly.
type StreamMapper struct {
	ID    string
	Model string

	startSent       bool
	currentBlockIdx int
	currentBlockOpen bool
	currentBlockKind string // "text" or "tool_use"
}

func NewStreamMapper(id, modelAlias string) *StreamMapper {
	return &StreamMapper{ID: id, Model: modelAlias, currentBlockIdx: -1}
}

func (m *StreamMapper) Start(promptTokens int) []FrameEvent {
	if m.startSent {
		return nil
	}
	m.startSent = true
	payload := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": m.ID, "type": "message", "role": "assistant", "model": m.Model,
			"content": []any{}, "usage": map[string]any{"input_tokens": promptTokens, "output_tokens": 0},
		},
	}
	b, _ := json.Marshal(payload)
	return []FrameEvent{{Event: "message_start", Data: string(b)}}
}

// Map produces 0..N frame events for a canonical event. Caller invokes
// Finish() once the underlying stream EOFs.
func (m *StreamMapper) Map(evt canonical.StreamEvent) []FrameEvent {
	var out []FrameEvent
	switch evt.Type {
	case canonical.StreamEventContent:
		if evt.ContentDelta == "" {
			return nil
		}
		out = append(out, m.ensureBlock("text")...)
		payload := map[string]any{
			"type":  "content_block_delta",
			"index": m.currentBlockIdx,
			"delta": map[string]any{"type": "text_delta", "text": evt.ContentDelta},
		}
		b, _ := json.Marshal(payload)
		out = append(out, FrameEvent{Event: "content_block_delta", Data: string(b)})
	case canonical.StreamEventToolCallStart:
		out = append(out, m.closeBlock()...)
		m.currentBlockIdx++
		m.currentBlockKind = "tool_use"
		m.currentBlockOpen = true
		payload := map[string]any{
			"type":  "content_block_start",
			"index": m.currentBlockIdx,
			"content_block": map[string]any{
				"type": "tool_use", "id": evt.ToolCallID, "name": evt.ToolCallName, "input": map[string]any{},
			},
		}
		b, _ := json.Marshal(payload)
		out = append(out, FrameEvent{Event: "content_block_start", Data: string(b)})
	case canonical.StreamEventToolCallDelta:
		payload := map[string]any{
			"type":  "content_block_delta",
			"index": m.currentBlockIdx,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": evt.ToolCallArgsDelta},
		}
		b, _ := json.Marshal(payload)
		out = append(out, FrameEvent{Event: "content_block_delta", Data: string(b)})
	case canonical.StreamEventStop:
		out = append(out, m.closeBlock()...)
		payload := map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": mapStopReason(evt.StopReason)},
			"usage": map[string]any{"output_tokens": 0},
		}
		if evt.Usage != nil {
			payload["usage"] = map[string]any{"output_tokens": evt.Usage.CompletionTokens}
		}
		b, _ := json.Marshal(payload)
		out = append(out, FrameEvent{Event: "message_delta", Data: string(b)})
		out = append(out, FrameEvent{Event: "message_stop", Data: `{"type":"message_stop"}`})
	}
	return out
}

func (m *StreamMapper) ensureBlock(kind string) []FrameEvent {
	if m.currentBlockOpen && m.currentBlockKind == kind {
		return nil
	}
	out := m.closeBlock()
	m.currentBlockIdx++
	m.currentBlockKind = kind
	m.currentBlockOpen = true
	payload := map[string]any{
		"type":  "content_block_start",
		"index": m.currentBlockIdx,
		"content_block": map[string]any{"type": kind, "text": ""},
	}
	b, _ := json.Marshal(payload)
	return append(out, FrameEvent{Event: "content_block_start", Data: string(b)})
}

func (m *StreamMapper) closeBlock() []FrameEvent {
	if !m.currentBlockOpen {
		return nil
	}
	m.currentBlockOpen = false
	payload := map[string]any{"type": "content_block_stop", "index": m.currentBlockIdx}
	b, _ := json.Marshal(payload)
	return []FrameEvent{{Event: "content_block_stop", Data: string(b)}}
}

// silence unused warning when fmt isn't needed; keep import for future detailed errors
var _ = fmt.Sprintf
```

- [ ] **Step 2: Commit**

```bash
go build ./internal/translate/anthropic/...
git add proxy/internal/translate/anthropic/stream.go
git commit -m "translate/anthropic: map canonical events to Anthropic typed SSE frames"
```

---

## Group D: Provider adapters — migrate to canonical (sequential per provider)

Each existing adapter currently uses the old flat `provider.Request{Model, Messages, MaxTokens}` shape. Phase 2 adds richer support but keeps the old types as aliases / fallbacks for the fast path.

### Task 10: Extend provider package types to canonical-aware

**Files:**
- Modify: `proxy/internal/provider/provider.go`

- [ ] **Step 1: Extend Provider/StreamingProvider interfaces**

The existing `Provider.Send(ctx, Request, apiKey)` and `StreamingProvider.SendStream(...)` keep their flat signatures (for the fast path), but we add canonical-aware variants:

```go
// (Append to provider.go)
import "github.com/admin/maas-router/proxy/internal/canonical"

// CanonicalProvider sends a canonical Request and returns either a complete
// Response or a StreamReader of canonical events. Adapters that support tool
// calls / vision implement this; the older Provider interface is kept for the
// fast path where the surface format matches the provider format.
type CanonicalProvider interface {
	SendCanonical(ctx context.Context, req canonical.Request, apiKey string) (*canonical.Response, error)
	SendCanonicalStream(ctx context.Context, req canonical.Request, apiKey string) (canonical.StreamReader, error)
}
```

Add to `canonical/response.go`:

```go
// StreamReader is the canonical event iterator. Adapters return one when
// streaming is requested. Recv returns io.EOF when the stream ends.
type StreamReader interface {
	Recv() (StreamEvent, error)
	Close() error
}
```

- [ ] **Step 2: Commit**

```bash
go build ./...
git add proxy/internal/provider/ proxy/internal/canonical/
git commit -m "provider+canonical: add CanonicalProvider interface and canonical.StreamReader"
```

---

### Task 11: OpenAI adapter — canonical support

**Files:**
- Modify: `proxy/internal/provider/openai/openai.go`

- [ ] **Step 1: Implement SendCanonical / SendCanonicalStream**

Build the OAI wire request from `canonical.Request` (the inverse of `translate/oai.ToCanonical`). Reuse the existing `Send` / `SendStream` plumbing but operate on canonical types end-to-end.

Key transformations:
- `canonical.System` → first message with role="system"
- `canonical.RoleTool` doesn't appear in canonical (we fold tool_result into user with content blocks); when serializing to OAI, emit a `{"role":"tool", "tool_call_id":..., "content":...}` message for each `tool_result` block
- `canonical.Tools` → OAI `tools: [{type:"function", function:{name, description, parameters}}]`
- `canonical.ToolChoice` → OAI `tool_choice`
- Vision: `canonical.BlockImage` → `{type:"image_url", image_url:{url}}`

Response mapping:
- OAI `choices[0].message.content` → one TextBlock
- OAI `choices[0].message.tool_calls[]` → one ToolUseBlock each (Arguments string into ToolInput)
- OAI `finish_reason` → canonical StopReason (`tool_calls` → `tool_use`)

Streaming: parse OAI deltas, emit canonical events. Track tool_call indexes so deltas attach to the right canonical ToolCallID.

(Full code omitted from this plan due to length — but follows the same structure as the existing `Send`/`SendStream` methods. Test against fixture OAI responses to verify roundtrip.)

- [ ] **Step 2: Test roundtrip (fixture-based)**

Create `proxy/internal/provider/openai/canonical_test.go` with one test that builds a canonical request with a tool call, builds the OAI wire request, asserts shape. (No live API call.)

- [ ] **Step 3: Commit**

```bash
go test ./internal/provider/openai/...
git add proxy/internal/provider/openai/
git commit -m "provider/openai: implement CanonicalProvider with tools + vision support"
```

---

### Task 12: Anthropic adapter — canonical support

**Files:**
- Modify: `proxy/internal/provider/anthropic/anthropic.go`

Same structure as Task 11 but for Anthropic. Key differences:
- System is already separate (just pass through)
- Tools format: `[{name, description, input_schema}]` (already lined up with canonical.Tool)
- Vision: `{type:"image", source:{type:"url"|"base64", url|data, media_type}}`
- Streaming: parse Anthropic's typed events (already done in the existing `anthropicStream` — extend to handle tool_use blocks and emit ToolCallStart/Delta/Stop canonical events).

Commit message:
```
provider/anthropic: implement CanonicalProvider with tools + vision support
```

---

### Task 13: Gemini adapter — canonical support

**Files:**
- Modify: `proxy/internal/provider/gemini/gemini.go`

Same structure. Gemini specifics:
- System: `systemInstruction: {parts: [{text}]}`
- Tools: `tools: [{functionDeclarations: [{name, description, parameters}]}]`
- Tool calls in response: `candidates[0].content.parts[].functionCall: {name, args}` — no per-tool ID; we synthesize one (`gemini-fn-<idx>`).
- Tool results sent back: `parts[].functionResponse: {name, response}` — anatomically different from a "tool message"; lower from canonical ToolResultBlock by emitting a part with the previous tool's name.
- Vision: `parts[].inlineData: {mimeType, data}` (base64) or `parts[].fileData: {mimeType, fileUri}`.

Commit message:
```
provider/gemini: implement CanonicalProvider with tools + vision support
```

---

## Group E: Anthropic surface handler (NEW endpoint)

### Task 14: POST /v1/messages handler

**Files:**
- Create: `proxy/internal/server/anthropic.go`
- Modify: `proxy/cmd/proxy/main.go`

- [ ] **Step 1: Implement the handler**

Pattern mirrors `server/openai.go`'s `ChatCompletions`:
1. Auth via bearer (Anthropic-style: `x-api-key` header preferred but we accept `Authorization: Bearer` for compatibility)
2. Parse `MessagesRequest`
3. Lower to canonical via `translate/anthropic.ToCanonical`
4. Catalog lookup; resolve provider
5. Tokenize via existing registry
6. Reserve
7. Branch on stream:
   - Non-streaming: provider.SendCanonical → translate/anthropic.FromCanonical → JSON response
   - Streaming: provider.SendCanonicalStream → translate/anthropic.StreamMapper → write typed SSE frames using the new SSE writer extension that supports `event: <name>` lines

- [ ] **Step 2: Extend stream writer for typed events**

The existing `stream/sse.go` only writes `data:` lines. Add `(*Writer) SendFrame(event, data string)` that writes:
```
event: <event>
data: <data>

```

- [ ] **Step 3: Wire route in main.go**

```go
mux.HandleFunc("/v1/messages", anthropicHandler.Messages)
```

- [ ] **Step 4: Smoke test the route**

```bash
curl -X POST http://localhost:8080/v1/messages \
  -H "x-api-key: $API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "anthropic/claude-haiku-4-5",
    "max_tokens": 50,
    "messages": [{"role":"user","content":"hi"}]
  }'
```

- [ ] **Step 5: Commit**

```bash
git add proxy/
git commit -m "server: add POST /v1/messages (Anthropic-compatible surface)"
```

---

## Group F: OpenAI handler refactor

### Task 15: Migrate /v1/chat/completions handler to canonical flow

**Files:**
- Modify: `proxy/internal/server/openai.go`

The existing handler parses OAI request locally and calls `provider.Send` with flat types. Refactor:
1. Parse OAI wire request
2. Call `translate/oai.ToCanonical`
3. Catalog + auth + reserve as today
4. Call `provider.SendCanonical` (use CanonicalProvider interface)
5. Stream branch: use `translate/oai.StreamMapper` to convert canonical events to OAI chunks before writing
6. Non-streaming: use `translate/oai.FromCanonical`

Keep the "fast path" decision deferred to Phase 3+ — for now, every request goes through canonical. The performance overhead is small (one struct copy + pointer chasing on content blocks) and gets us a clean baseline.

Commit:
```
server/openai: delegate to canonical via translate/oai for tools+vision support
```

---

## Group G: Catalog + integration tests

### Task 16: Update model_catalog flags

**Files:**
- Modify: `web/prisma/seed.ts`

Set accurate `supports_tools` and `supports_vision` for each model:

| Alias | Tools | Vision |
|---|---|---|
| openai/gpt-4o | ✓ | ✓ |
| openai/gpt-4o-mini | ✓ | ✓ |
| openai/o1 | ✗ | ✗ |
| anthropic/claude-sonnet-4-6 | ✓ | ✓ |
| anthropic/claude-haiku-4-5 | ✓ | ✓ |
| google/gemini-2.5-pro | ✓ | ✓ |
| google/gemini-2.5-flash | ✓ | ✓ |

Update the `supportsTools` and `supportsVision` values in `additionalModels` and the original GPT-4o block. Re-run `pnpm prisma db seed`.

Commit:
```
db: set accurate supports_tools / supports_vision flags per model
```

---

### Task 17: Cross-format integration smoke test

**Files:**
- Modify: `scripts/smoke-test.sh`

Add cases for each (surface, provider) combination:

```bash
# 1. OAI surface + OAI provider (existing)
# 2. OAI surface + Anthropic provider (cross-format)
# 3. OAI surface + Gemini provider (cross-format)
# 4. Anthropic surface + Anthropic provider (NEW endpoint, native)
# 5. Anthropic surface + OAI provider (NEW endpoint, cross-format)
# 6. Anthropic surface + Gemini provider (NEW endpoint, cross-format)

# Each case: simple chat, verify response shape, verify request_logs.status='success'
```

Plus a tool-call case for at least one combination:
```bash
# Tool call: OAI surface + Anthropic provider
curl -sS -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "anthropic/claude-haiku-4-5",
    "messages": [{"role":"user","content":"What is the weather in Paris?"}],
    "tools": [{
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "Get current weather for a city",
        "parameters": {"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}
      }
    }],
    "max_tokens": 200
  }' | jq '.choices[0].message.tool_calls[0]'
```

Plus a vision case:
```bash
# Vision: OAI surface + Gemini provider (cheapest vision model)
curl -sS -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "google/gemini-2.5-flash",
    "messages": [{"role":"user","content":[
      {"type":"text","text":"What color is this?"},
      {"type":"image_url","image_url":{"url":"https://upload.wikimedia.org/wikipedia/commons/thumb/4/4f/Red_square.png/120px-Red_square.png"}}
    ]}],
    "max_tokens": 30
  }' | jq '.choices[0].message.content'
```

Commit:
```
scripts: add smoke-test cases for all 6 (surface, provider) combos + tool call + vision
```

---

## Self-review

Run through spec §6 and confirm coverage:

- [ ] §6.1 5-layer architecture: surface → translate → canonical → provider adapter → upstream ✓ (Tasks 1-15)
- [ ] §6.2 Fast path for same-format pairs: explicitly deferred to Phase 3 (acceptable trade-off; canonical adds <5ms latency)
- [ ] §6.3 Feature support matrix:
  - Basic chat: ✓ all 6 combos
  - Streaming: ✓ all 6 combos (Anthropic surface streaming = new in Task 9)
  - Tools: ✓ all 6 combos (Tasks 11-13)
  - Vision: ✓ all 6 combos (Tasks 11-13)
  - JSON mode: ✗ deferred
  - Logprobs: ✗ deferred
- [ ] §6.4 Go package layout: matches new `canonical/`, `translate/oai/`, `translate/anthropic/` packages
- [ ] §6.5 Canonical IR sketch: implemented in Tasks 1-3 (richer than the sketch — adds ImageData byte path)
- [ ] §6.6 Provider interface: extended with CanonicalProvider (Task 10)
- [ ] §6.7 Testing strategy: pure-function golden tests for translators (Tasks 4, 7); integration smoke for cross-format (Task 17)
- [ ] §6.8 Translation challenges: addressed — OAI tools↔Anthropic schema (canonical.Tool); tool result role-folding (Tasks 4, 7); SSE event shapes (Tasks 6, 9)

## Risks specific to Phase 2

- **Tool call ID stability**: OAI uses `call_<random>`, Anthropic uses `toolu_<random>`, Gemini has no native ID. The canonical pipes the OAI/Anthropic IDs through and synthesizes for Gemini (`gemini-fn-<idx>`). Multi-turn agentic flows that round-trip through translation may see ID instability if the same canonical request goes through two providers. **Mitigation**: log both `request_logs.upstream_tool_call_id` and surface-side ID in v3 if this becomes a debugging problem. Phase 2 accepts this.
- **Anthropic SSE typed events**: clients written against Anthropic SDK expect named events. The new `Writer.SendFrame` must produce them with no `\r\n` line endings (LF only) and a trailing blank line. Test against the official `anthropic-python` SDK to verify.
- **Streaming JSON arg deltas**: OAI sends arguments as opaque string deltas across multiple chunks. Anthropic sends `partial_json` deltas. Our canonical `ToolCallArgsDelta` is just a string; the OAI-surface stream mapper concatenates them. If a client parses the arguments before the tool_call_stop, they get partial JSON — that's a client problem, not ours.
- **Gemini function-call streaming**: Gemini doesn't stream function calls incrementally — the full `functionCall` object arrives in a single chunk. We emit start + delta-with-full-args + stop in immediate sequence. Acceptable but a quirk.

## Parallelism notes

- **Wave 1 (sequential):** Tasks 1, 2, 3 (canonical IR types). Task 3 depends on 1 (uses ContentBlock).
- **Wave 2 (parallel):** Tasks 4, 5, 6 (OAI translator) and Tasks 7, 8, 9 (Anthropic translator) — 6 independent files.
- **Wave 3 (sequential):** Task 10 (Provider interface extension).
- **Wave 4 (parallel):** Tasks 11, 12, 13 (three provider adapters) — independent packages.
- **Wave 5 (sequential):** Tasks 14 (Anthropic handler), 15 (OAI handler refactor).
- **Wave 6 (parallel):** Tasks 16 (catalog seed), 17 (smoke test) — independent files.

Estimated subagent waves: 6. Total tasks: 17. Estimated solo-dev time: 3 weeks per spec §11 (matches), or ~3-4 hours of subagent execution given parallelism.

---

**End of Phase 2 plan.**
