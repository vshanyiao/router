package openai

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	openaisdk "github.com/sashabaranov/go-openai"
	"github.com/admin/maas-router/proxy/internal/canonical"
	"github.com/admin/maas-router/proxy/internal/provider"
)

type Adapter struct{}

func New() *Adapter { return &Adapter{} }

func (a *Adapter) Send(ctx context.Context, req provider.Request, apiKey string) (*provider.Response, error) {
	if apiKey == "" {
		return nil, errors.New("openai api key not configured")
	}
	client := openaisdk.NewClient(apiKey)

	var msgs []openaisdk.ChatCompletionMessage
	for _, m := range req.Messages {
		msgs = append(msgs, openaisdk.ChatCompletionMessage{Role: m.Role, Content: m.Content})
	}

	resp, err := client.CreateChatCompletion(ctx, openaisdk.ChatCompletionRequest{
		Model:     req.Model,
		Messages:  msgs,
		MaxTokens: req.MaxTokens,
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, errors.New("no choices returned")
	}
	return &provider.Response{
		Content:          resp.Choices[0].Message.Content,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
	}, nil
}

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
		Model:         req.Model,
		Messages:      msgs,
		MaxTokens:     req.MaxTokens,
		Stream:        true,
		StreamOptions: &openaisdk.StreamOptions{IncludeUsage: true},
	})
	if err != nil {
		return nil, err
	}
	return &streamReader{src: stream}, nil
}

// ---- canonical provider methods ----

func (a *Adapter) SendCanonical(ctx context.Context, req canonical.Request, apiKey string) (*canonical.Response, error) {
	if apiKey == "" {
		return nil, errors.New("openai api key not configured")
	}
	client := openaisdk.NewClient(apiKey)
	oaiReq, err := buildOAIRequest(req, false)
	if err != nil {
		return nil, err
	}
	resp, err := client.CreateChatCompletion(ctx, oaiReq)
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, errors.New("no choices returned")
	}
	return openAIRespToCanonical(resp), nil
}

func (a *Adapter) SendCanonicalStream(ctx context.Context, req canonical.Request, apiKey string) (canonical.StreamReader, error) {
	if apiKey == "" {
		return nil, errors.New("openai api key not configured")
	}
	client := openaisdk.NewClient(apiKey)
	oaiReq, err := buildOAIRequest(req, true)
	if err != nil {
		return nil, err
	}
	oaiReq.StreamOptions = &openaisdk.StreamOptions{IncludeUsage: true}
	stream, err := client.CreateChatCompletionStream(ctx, oaiReq)
	if err != nil {
		return nil, err
	}
	return &canonicalStream{src: stream, toolIndexToID: map[int]string{}}, nil
}

// ---- helpers ----

func buildOAIRequest(req canonical.Request, stream bool) (openaisdk.ChatCompletionRequest, error) {
	var msgs []openaisdk.ChatCompletionMessage

	if req.System != "" {
		msgs = append(msgs, openaisdk.ChatCompletionMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	for _, m := range req.Messages {
		converted, err := canonicalMsgToOAI(m)
		if err != nil {
			return openaisdk.ChatCompletionRequest{}, err
		}
		msgs = append(msgs, converted...)
	}

	oaiReq := openaisdk.ChatCompletionRequest{
		Model:     req.Model,
		Messages:  msgs,
		MaxTokens: req.MaxTokens,
		Stop:      req.StopSequences,
		Stream:    stream,
	}

	if req.Temperature != nil {
		oaiReq.Temperature = float32(*req.Temperature)
	}
	if req.TopP != nil {
		oaiReq.TopP = float32(*req.TopP)
	}

	if len(req.Tools) > 0 {
		oaiTools := make([]openaisdk.Tool, len(req.Tools))
		for i, t := range req.Tools {
			oaiTools[i] = openaisdk.Tool{
				Type: openaisdk.ToolTypeFunction,
				Function: &openaisdk.FunctionDefinition{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.InputSchema,
				},
			}
		}
		oaiReq.Tools = oaiTools
	}

	if req.ToolChoice.Mode != "" {
		oaiReq.ToolChoice = canonicalToolChoiceToOAI(req.ToolChoice)
	}

	return oaiReq, nil
}

// canonicalMsgToOAI converts one canonical.Message into zero or more OAI messages.
// A user message that contains tool results emits a separate "tool" role message
// per result block (OAI requires role=tool with tool_call_id).
func canonicalMsgToOAI(m canonical.Message) ([]openaisdk.ChatCompletionMessage, error) {
	switch m.Role {
	case canonical.RoleUser:
		return userMsgToOAI(m)
	case canonical.RoleAssistant:
		return assistantMsgToOAI(m)
	case canonical.RoleTool:
		return toolMsgToOAI(m)
	default:
		return nil, fmt.Errorf("unknown role: %s", m.Role)
	}
}

func userMsgToOAI(m canonical.Message) ([]openaisdk.ChatCompletionMessage, error) {
	var parts []openaisdk.ChatMessagePart
	var toolMsgs []openaisdk.ChatCompletionMessage

	for _, blk := range m.Content {
		switch blk.Type {
		case canonical.BlockText:
			parts = append(parts, openaisdk.ChatMessagePart{
				Type: openaisdk.ChatMessagePartTypeText,
				Text: blk.Text,
			})
		case canonical.BlockImage:
			url := blk.ImageURL
			if url == "" && len(blk.ImageData) > 0 {
				url = "data:" + blk.MediaType + ";base64," + base64.StdEncoding.EncodeToString(blk.ImageData)
			}
			parts = append(parts, openaisdk.ChatMessagePart{
				Type:     openaisdk.ChatMessagePartTypeImageURL,
				ImageURL: &openaisdk.ChatMessageImageURL{URL: url},
			})
		case canonical.BlockToolResult:
			toolMsgs = append(toolMsgs, openaisdk.ChatCompletionMessage{
				Role:       "tool",
				Content:    blk.ToolResult,
				ToolCallID: blk.ToolUseID,
			})
		}
	}

	var result []openaisdk.ChatCompletionMessage
	if len(parts) > 0 {
		msg := openaisdk.ChatCompletionMessage{Role: "user"}
		if len(parts) == 1 && parts[0].Type == openaisdk.ChatMessagePartTypeText {
			msg.Content = parts[0].Text
		} else {
			msg.MultiContent = parts
		}
		result = append(result, msg)
	}
	result = append(result, toolMsgs...)
	return result, nil
}

func assistantMsgToOAI(m canonical.Message) ([]openaisdk.ChatCompletionMessage, error) {
	msg := openaisdk.ChatCompletionMessage{Role: "assistant"}
	for _, blk := range m.Content {
		switch blk.Type {
		case canonical.BlockText:
			msg.Content += blk.Text
		case canonical.BlockToolUse:
			msg.ToolCalls = append(msg.ToolCalls, openaisdk.ToolCall{
				ID:   blk.ToolUseID,
				Type: openaisdk.ToolTypeFunction,
				Function: openaisdk.FunctionCall{
					Name:      blk.ToolName,
					Arguments: string(blk.ToolInput),
				},
			})
		}
	}
	return []openaisdk.ChatCompletionMessage{msg}, nil
}

func toolMsgToOAI(m canonical.Message) ([]openaisdk.ChatCompletionMessage, error) {
	var msgs []openaisdk.ChatCompletionMessage
	for _, blk := range m.Content {
		if blk.Type == canonical.BlockToolResult {
			msgs = append(msgs, openaisdk.ChatCompletionMessage{
				Role:       "tool",
				Content:    blk.ToolResult,
				ToolCallID: blk.ToolUseID,
			})
		}
	}
	return msgs, nil
}

func canonicalToolChoiceToOAI(tc canonical.ToolChoice) any {
	switch tc.Mode {
	case canonical.ToolChoiceNone:
		return "none"
	case canonical.ToolChoiceAny:
		return "required"
	case canonical.ToolChoiceSpecific:
		return openaisdk.ToolChoice{
			Type:     openaisdk.ToolTypeFunction,
			Function: openaisdk.ToolFunction{Name: tc.ToolName},
		}
	default: // ToolChoiceAuto or empty
		return "auto"
	}
}

func openAIRespToCanonical(resp openaisdk.ChatCompletionResponse) *canonical.Response {
	choice := resp.Choices[0]
	var blocks []canonical.ContentBlock

	if choice.Message.Content != "" {
		blocks = append(blocks, canonical.TextBlock(choice.Message.Content))
	}
	for _, tc := range choice.Message.ToolCalls {
		blocks = append(blocks, canonical.ToolUseBlock(
			tc.ID,
			tc.Function.Name,
			[]byte(tc.Function.Arguments),
		))
	}

	stopReason := mapFinishReason(string(choice.FinishReason))

	return &canonical.Response{
		Content:    blocks,
		StopReason: stopReason,
		Usage: canonical.Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
		},
	}
}

func mapFinishReason(r string) string {
	switch r {
	case "tool_calls":
		return "tool_use"
	case "length":
		return "length"
	default:
		return "stop"
	}
}

// canonicalStream implements canonical.StreamReader over an OAI stream.
type canonicalStream struct {
	src           *openaisdk.ChatCompletionStream
	toolIndexToID map[int]string
	stopEmitted   bool
	// buffered stop event when io.EOF is received from src before finish_reason
	pendingStop *canonical.StreamEvent
}

func (s *canonicalStream) Recv() (canonical.StreamEvent, error) {
	// If we already emitted the stop event, signal EOF.
	if s.stopEmitted {
		return canonical.StreamEvent{}, io.EOF
	}

	// Drain any pending stop event (from a previous EOF on the source).
	if s.pendingStop != nil {
		ev := *s.pendingStop
		s.pendingStop = nil
		s.stopEmitted = true
		return ev, io.EOF
	}

	for {
		resp, err := s.src.Recv()
		if err == io.EOF {
			// Source exhausted without a finish_reason chunk — emit a bare stop.
			stopEv := canonical.StreamEvent{Type: canonical.StreamEventStop, StopReason: "stop"}
			s.stopEmitted = true
			return stopEv, io.EOF
		}
		if err != nil {
			return canonical.StreamEvent{Type: canonical.StreamEventError, Error: &canonical.ErrorInfo{Message: err.Error()}}, err
		}

		// Accumulate usage if present.
		var usagePtr *canonical.Usage
		if resp.Usage != nil {
			usagePtr = &canonical.Usage{
				PromptTokens:     resp.Usage.PromptTokens,
				CompletionTokens: resp.Usage.CompletionTokens,
			}
		}

		if len(resp.Choices) == 0 {
			// usage-only chunk — emit usage event if we have usage, otherwise skip.
			if usagePtr != nil {
				return canonical.StreamEvent{Type: canonical.StreamEventUsage, Usage: usagePtr}, nil
			}
			continue
		}

		choice := resp.Choices[0]
		delta := choice.Delta

		// Finish reason chunk.
		if choice.FinishReason != "" {
			stopReason := mapFinishReason(string(choice.FinishReason))
			ev := canonical.StreamEvent{
				Type:       canonical.StreamEventStop,
				StopReason: stopReason,
				Usage:      usagePtr,
			}
			s.stopEmitted = true
			return ev, io.EOF
		}

		// Content delta.
		if delta.Content != "" {
			return canonical.StreamEvent{
				Type:         canonical.StreamEventContent,
				ContentDelta: delta.Content,
			}, nil
		}

		// Tool call deltas.
		if len(delta.ToolCalls) > 0 {
			tc := delta.ToolCalls[0]
			idx := 0
			if tc.Index != nil {
				idx = *tc.Index
			}

			if tc.ID != "" && tc.Function.Name != "" {
				// Start of a new tool call.
				s.toolIndexToID[idx] = tc.ID
				return canonical.StreamEvent{
					Type:         canonical.StreamEventToolCallStart,
					ToolCallID:   tc.ID,
					ToolCallName: tc.Function.Name,
				}, nil
			}

			if tc.Function.Arguments != "" {
				id := s.toolIndexToID[idx]
				return canonical.StreamEvent{
					Type:              canonical.StreamEventToolCallDelta,
					ToolCallID:        id,
					ToolCallArgsDelta: tc.Function.Arguments,
				}, nil
			}
		}

		// Empty/no-op chunk — loop again.
	}
}

func (s *canonicalStream) Close() error { return s.src.Close() }
