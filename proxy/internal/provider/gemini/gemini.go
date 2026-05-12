package gemini

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/admin/maas-router/proxy/internal/canonical"
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
	body   io.ReadCloser
	buf    []byte
	usage  provider.Usage
	done   bool
	atEOF  bool // body returned io.EOF; finish draining buf then emit stop
}

// parseEvent extracts content delta from one SSE event payload (the bytes
// between two `\n\n` boundaries). Returns the content text (or "" if the
// event had no content) and updates usage in-place when present.
func (s *geminiStream) parseEvent(chunk []byte) string {
	var data []byte
	for _, line := range bytes.Split(chunk, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("data: ")) {
			data = append(data, line[len("data: "):]...)
		}
	}
	if len(data) == 0 {
		return ""
	}
	var partial genResp
	if err := json.Unmarshal(data, &partial); err != nil {
		return ""
	}
	if partial.UsageMetadata.PromptTokenCount > 0 {
		s.usage.PromptTokens = partial.UsageMetadata.PromptTokenCount
	}
	if partial.UsageMetadata.CandidatesTokenCount > 0 {
		s.usage.CompletionTokens = partial.UsageMetadata.CandidatesTokenCount
	}
	if len(partial.Candidates) > 0 && len(partial.Candidates[0].Content.Parts) > 0 {
		return partial.Candidates[0].Content.Parts[0].Text
	}
	return ""
}

func (s *geminiStream) Recv() (provider.StreamEvent, error) {
	if s.done {
		return provider.StreamEvent{}, io.EOF
	}
	tmp := make([]byte, 1024)
	for {
		// Look for a complete event in the buffer first.
		if idx := bytes.Index(s.buf, []byte("\n\n")); idx >= 0 {
			chunk := s.buf[:idx]
			s.buf = append([]byte{}, s.buf[idx+2:]...)
			if text := s.parseEvent(chunk); text != "" {
				return provider.StreamEvent{Type: "content", ContentDelta: text}, nil
			}
			continue
		}
		// Buffer has no complete event. If we already saw EOF, try to parse
		// any tailing event without trailing `\n\n` (some servers omit it),
		// then emit stop. This is the fix for C1 — we MUST drain remaining
		// data after EOF, because Gemini's usageMetadata event arrives in
		// the same Read that returns EOF.
		if s.atEOF {
			if len(s.buf) > 0 {
				chunk := s.buf
				s.buf = nil
				if text := s.parseEvent(chunk); text != "" {
					return provider.StreamEvent{Type: "content", ContentDelta: text}, nil
				}
			}
			s.done = true
			u := s.usage
			return provider.StreamEvent{Type: "stop", StopReason: "stop", Usage: &u}, io.EOF
		}
		// Read more from body. (n>0, EOF) is legal HTTP/1.1 end-of-stream —
		// we set atEOF and loop back to drain the buffer.
		n, err := s.body.Read(tmp)
		if n > 0 {
			s.buf = append(s.buf, tmp[:n]...)
		}
		if err == io.EOF {
			s.atEOF = true
			continue
		}
		if err != nil {
			return provider.StreamEvent{}, err
		}
	}
}

func (s *geminiStream) Close() error { return s.body.Close() }

// ---------------------------------------------------------------------------
// Canonical wire types — used only by SendCanonical / SendCanonicalStream.
// The existing genReq/genResp/content/part types are kept unchanged above.
// ---------------------------------------------------------------------------

// canonPart is a richer version of part that covers all block kinds.
type canonPart struct {
	Text             string            `json:"text,omitempty"`
	InlineData       *inlineData       `json:"inlineData,omitempty"`
	FileData         *fileData         `json:"fileData,omitempty"`
	FunctionCall     *functionCall     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponse `json:"functionResponse,omitempty"`
}

type inlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // base64-encoded
}

type fileData struct {
	MimeType string `json:"mimeType"`
	FileURI  string `json:"fileUri"`
}

type functionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type functionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

// canonContent is like content but uses canonPart.
type canonContent struct {
	Role  string      `json:"role,omitempty"`
	Parts []canonPart `json:"parts"`
}

// canonGenCfg extends genCfg with temperature, topP, stopSequences.
type canonGenCfg struct {
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type funcDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type canonTool struct {
	FunctionDeclarations []funcDecl `json:"functionDeclarations"`
}

type funcCallingCfg struct {
	Mode                 string   `json:"mode"`
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

type canonToolConfig struct {
	FunctionCallingConfig funcCallingCfg `json:"functionCallingConfig"`
}

type canonGenReq struct {
	Contents          []canonContent `json:"contents"`
	SystemInstruction *canonContent  `json:"systemInstruction,omitempty"`
	GenerationConfig  *canonGenCfg   `json:"generationConfig,omitempty"`
	Tools             []canonTool    `json:"tools,omitempty"`
	ToolConfig        *canonToolConfig `json:"toolConfig,omitempty"`
}

// canonGenResp overlaps genResp but uses canonPart for richer part decoding.
type canonGenResp struct {
	Candidates []struct {
		Content      canonContent `json:"content"`
		FinishReason string       `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildToolUseIDMap scans all messages and returns a map of toolUseID → toolName
// collected from BlockToolUse blocks in assistant messages.
func buildToolUseIDMap(msgs []canonical.Message) map[string]string {
	m := make(map[string]string)
	for _, msg := range msgs {
		if msg.Role != canonical.RoleAssistant {
			continue
		}
		for _, blk := range msg.Content {
			if blk.Type == canonical.BlockToolUse && blk.ToolUseID != "" {
				m[blk.ToolUseID] = blk.ToolName
			}
		}
	}
	return m
}

// mimeOrDefault returns the media type, falling back to "image/png".
func mimeOrDefault(mt string) string {
	if mt == "" {
		return "image/png"
	}
	return mt
}

// buildCanonReq converts a canonical.Request into the Gemini wire format.
func buildCanonReq(req canonical.Request) canonGenReq {
	toolUseIDToName := buildToolUseIDMap(req.Messages)

	body := canonGenReq{}

	// system instruction
	if req.System != "" {
		body.SystemInstruction = &canonContent{Parts: []canonPart{{Text: req.System}}}
	}

	// contents
	for _, msg := range req.Messages {
		role := string(msg.Role)
		if role == "assistant" {
			role = "model"
		}
		// RoleTool folds into user messages per spec.
		if role == "tool" {
			role = "user"
		}
		var parts []canonPart
		for _, blk := range msg.Content {
			switch blk.Type {
			case canonical.BlockText:
				parts = append(parts, canonPart{Text: blk.Text})
			case canonical.BlockImage:
				if len(blk.ImageData) > 0 {
					parts = append(parts, canonPart{
						InlineData: &inlineData{
							MimeType: mimeOrDefault(blk.MediaType),
							Data:     base64.StdEncoding.EncodeToString(blk.ImageData),
						},
					})
				} else {
					// URL image — use fileData
					parts = append(parts, canonPart{
						FileData: &fileData{
							MimeType: mimeOrDefault(blk.MediaType),
							FileURI:  blk.ImageURL,
						},
					})
				}
			case canonical.BlockToolUse:
				parts = append(parts, canonPart{
					FunctionCall: &functionCall{
						Name: blk.ToolName,
						Args: blk.ToolInput,
					},
				})
			case canonical.BlockToolResult:
				name := blk.ToolUseID // fallback: use ID as name
				if n, ok := toolUseIDToName[blk.ToolUseID]; ok {
					name = n
				}
				parts = append(parts, canonPart{
					FunctionResponse: &functionResponse{
						Name:     name,
						Response: map[string]interface{}{"result": blk.ToolResult},
					},
				})
			}
		}
		if len(parts) == 0 {
			continue
		}
		body.Contents = append(body.Contents, canonContent{Role: role, Parts: parts})
	}

	// generationConfig
	cfg := &canonGenCfg{
		MaxOutputTokens: req.MaxTokens,
		Temperature:     req.Temperature,
		TopP:            req.TopP,
		StopSequences:   req.StopSequences,
	}
	if cfg.MaxOutputTokens > 0 || cfg.Temperature != nil || cfg.TopP != nil || len(cfg.StopSequences) > 0 {
		body.GenerationConfig = cfg
	}

	// tools
	if len(req.Tools) > 0 {
		decls := make([]funcDecl, 0, len(req.Tools))
		for _, t := range req.Tools {
			decls = append(decls, funcDecl{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			})
		}
		body.Tools = []canonTool{{FunctionDeclarations: decls}}
	}

	// toolConfig
	if req.ToolChoice.Mode != "" {
		var mode string
		var allowed []string
		switch req.ToolChoice.Mode {
		case canonical.ToolChoiceAuto:
			mode = "AUTO"
		case canonical.ToolChoiceNone:
			mode = "NONE"
		case canonical.ToolChoiceAny:
			mode = "ANY"
		case canonical.ToolChoiceSpecific:
			mode = "ANY"
			if req.ToolChoice.ToolName != "" {
				allowed = []string{req.ToolChoice.ToolName}
			}
		default:
			mode = "AUTO"
		}
		body.ToolConfig = &canonToolConfig{
			FunctionCallingConfig: funcCallingCfg{
				Mode:                 mode,
				AllowedFunctionNames: allowed,
			},
		}
	}

	return body
}

// canonRespToCanonical converts a canonGenResp into a canonical.Response.
func canonRespToCanonical(out canonGenResp) *canonical.Response {
	resp := &canonical.Response{
		Usage: canonical.Usage{
			PromptTokens:     out.UsageMetadata.PromptTokenCount,
			CompletionTokens: out.UsageMetadata.CandidatesTokenCount,
		},
	}

	if len(out.Candidates) == 0 {
		resp.StopReason = "stop"
		return resp
	}

	cand := out.Candidates[0]
	fnIdx := 0
	for _, p := range cand.Content.Parts {
		switch {
		case p.Text != "":
			resp.Content = append(resp.Content, canonical.TextBlock(p.Text))
		case p.FunctionCall != nil:
			id := fmt.Sprintf("gemini-fn-%d", fnIdx)
			fnIdx++
			argsJSON := p.FunctionCall.Args
			if argsJSON == nil {
				argsJSON = json.RawMessage("{}")
			}
			resp.Content = append(resp.Content, canonical.ToolUseBlock(id, p.FunctionCall.Name, argsJSON))
		}
	}

	// Determine stop reason
	switch cand.FinishReason {
	case "MAX_TOKENS":
		resp.StopReason = "length"
	default:
		resp.StopReason = "stop"
	}
	// If any tool_use blocks, override to "tool_use"
	for _, blk := range resp.Content {
		if blk.Type == canonical.BlockToolUse {
			resp.StopReason = "tool_use"
			break
		}
	}

	return resp
}

// ---------------------------------------------------------------------------
// CanonicalProvider implementation
// ---------------------------------------------------------------------------

func (a *Adapter) SendCanonical(ctx context.Context, req canonical.Request, apiKey string) (*canonical.Response, error) {
	if apiKey == "" {
		return nil, errors.New("gemini api key not configured")
	}
	url := fmt.Sprintf("%s/%s:generateContent", baseURL, req.Model)
	buf, _ := json.Marshal(buildCanonReq(req))
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
	var out canonGenResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return canonRespToCanonical(out), nil
}

func (a *Adapter) SendCanonicalStream(ctx context.Context, req canonical.Request, apiKey string) (canonical.StreamReader, error) {
	if apiKey == "" {
		return nil, errors.New("gemini api key not configured")
	}
	url := fmt.Sprintf("%s/%s:streamGenerateContent?alt=sse", baseURL, req.Model)
	buf, _ := json.Marshal(buildCanonReq(req))
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
	return &geminiCanonStream{body: resp.Body}, nil
}

// geminiCanonStream implements canonical.StreamReader over Gemini SSE.
type geminiCanonStream struct {
	body  io.ReadCloser
	buf   []byte
	usage canonical.Usage
	done  bool
	atEOF bool

	// pending events buffered from a single parsed chunk (e.g. tool call emits 3)
	pending []canonical.StreamEvent
}

func (s *geminiCanonStream) Close() error { return s.body.Close() }

func (s *geminiCanonStream) parseCanonEvents(chunk []byte) []canonical.StreamEvent {
	var data []byte
	for _, line := range bytes.Split(chunk, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("data: ")) {
			data = append(data, line[len("data: "):]...)
		}
	}
	if len(data) == 0 {
		return nil
	}
	var partial canonGenResp
	if err := json.Unmarshal(data, &partial); err != nil {
		return nil
	}
	if partial.UsageMetadata.PromptTokenCount > 0 {
		s.usage.PromptTokens = partial.UsageMetadata.PromptTokenCount
	}
	if partial.UsageMetadata.CandidatesTokenCount > 0 {
		s.usage.CompletionTokens = partial.UsageMetadata.CandidatesTokenCount
	}
	if len(partial.Candidates) == 0 {
		return nil
	}
	var events []canonical.StreamEvent
	fnIdx := 0
	for _, p := range partial.Candidates[0].Content.Parts {
		switch {
		case p.Text != "":
			events = append(events, canonical.StreamEvent{
				Type:         canonical.StreamEventContent,
				ContentDelta: p.Text,
			})
		case p.FunctionCall != nil:
			id := fmt.Sprintf("gemini-fn-%d", fnIdx)
			fnIdx++
			argsJSON := p.FunctionCall.Args
			if argsJSON == nil {
				argsJSON = json.RawMessage("{}")
			}
			events = append(events,
				canonical.StreamEvent{Type: canonical.StreamEventToolCallStart, ToolCallID: id, ToolCallName: p.FunctionCall.Name},
				canonical.StreamEvent{Type: canonical.StreamEventToolCallDelta, ToolCallID: id, ToolCallArgsDelta: string(argsJSON)},
				canonical.StreamEvent{Type: canonical.StreamEventToolCallStop, ToolCallID: id},
			)
		}
	}
	return events
}

func (s *geminiCanonStream) Recv() (canonical.StreamEvent, error) {
	if s.done {
		return canonical.StreamEvent{}, io.EOF
	}
	// Drain any buffered events from the last parsed chunk.
	if len(s.pending) > 0 {
		ev := s.pending[0]
		s.pending = s.pending[1:]
		return ev, nil
	}
	tmp := make([]byte, 1024)
	for {
		// Look for a complete SSE event in the buffer.
		if idx := bytes.Index(s.buf, []byte("\n\n")); idx >= 0 {
			chunk := s.buf[:idx]
			s.buf = append([]byte{}, s.buf[idx+2:]...)
			events := s.parseCanonEvents(chunk)
			if len(events) > 0 {
				ev := events[0]
				s.pending = events[1:]
				return ev, nil
			}
			continue
		}
		// No complete event yet. On EOF, drain remainder then emit stop.
		if s.atEOF {
			if len(s.buf) > 0 {
				chunk := s.buf
				s.buf = nil
				events := s.parseCanonEvents(chunk)
				if len(events) > 0 {
					ev := events[0]
					s.pending = events[1:]
					return ev, nil
				}
			}
			s.done = true
			u := s.usage
			return canonical.StreamEvent{Type: canonical.StreamEventStop, StopReason: "stop", Usage: &u}, io.EOF
		}
		// Read more bytes.
		n, err := s.body.Read(tmp)
		if n > 0 {
			s.buf = append(s.buf, tmp[:n]...)
		}
		if err == io.EOF {
			s.atEOF = true
			continue
		}
		if err != nil {
			return canonical.StreamEvent{}, err
		}
	}
}
