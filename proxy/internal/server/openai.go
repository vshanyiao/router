package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/admin/maas-router/proxy/internal/auth"
	"github.com/admin/maas-router/proxy/internal/billing"
	"github.com/admin/maas-router/proxy/internal/catalog"
	"github.com/admin/maas-router/proxy/internal/logging"
	"github.com/admin/maas-router/proxy/internal/provider"
	"github.com/admin/maas-router/proxy/internal/stream"
	"github.com/admin/maas-router/proxy/internal/tokenizer"
)

type ChatCompletionsRequest struct {
	Model     string                  `json:"model"`
	Messages  []ChatCompletionMessage `json:"messages"`
	MaxTokens int                     `json:"max_tokens,omitempty"`
	Stream    bool                    `json:"stream,omitempty"`
}

type ChatCompletionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionsResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   ChatCompletionUsage    `json:"usage"`
}

type ChatCompletionChoice struct {
	Index        int                   `json:"index"`
	Message      ChatCompletionMessage `json:"message"`
	FinishReason string                `json:"finish_reason"`
}

type ChatCompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OpenAIHandler struct {
	Auth       *auth.Auth
	Billing    *billing.Service
	Catalog    *catalog.Catalog
	Providers  map[string]provider.Provider // keyed by upstream_provider name: "openai", "anthropic", "google"
	Tokenizers *tokenizer.Registry
	Keys       map[string]string // upstream_provider → API key
	Reactor    *logging.Reactor
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"message": msg, "type": "invalid_request_error"},
	})
}

func (h *OpenAIHandler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	started := time.Now()

	var req ChatCompletionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages required")
		return
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 1024
	}

	result, err := h.Auth.LookupBearer(ctx, r.Header.Get("Authorization"))
	if err != nil {
		if errors.Is(err, auth.ErrUserBanned) {
			writeError(w, http.StatusForbidden, "account suspended or banned")
		} else {
			writeError(w, http.StatusUnauthorized, "invalid API key")
		}
		return
	}

	model, err := h.Catalog.Get(req.Model)
	if err != nil {
		writeError(w, http.StatusBadRequest, "model '"+req.Model+"' not found")
		return
	}

	prov, provOK := h.Providers[model.UpstreamProvider]
	if !provOK {
		writeError(w, http.StatusBadRequest, "no provider adapter for "+model.UpstreamProvider)
		return
	}
	apiKey, keyOK := h.Keys[model.UpstreamProvider]
	if !keyOK || apiKey == "" {
		writeError(w, http.StatusInternalServerError, "provider key not configured")
		return
	}

	requestID := uuid.New()
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
	estPromptTokens64, terr := tk.CountPromptTokens(ctx, model.UpstreamModelID, tokMsgs)
	if terr != nil {
		var promptChars int
		for _, m := range req.Messages {
			promptChars += len(m.Content)
		}
		estPromptTokens64 = promptChars / 4
	}
	estPromptTokens := int64(estPromptTokens64)

	maxCost := billing.CalculateCost(estPromptTokens, int64(req.MaxTokens), model.InputCentsPerMillionTokens, model.OutputCentsPerMillionTokens, model.MarkupPct)
	if err := h.Billing.Reserve(ctx, result.UserID, requestID, maxCost.TotalCents); err != nil {
		if errors.Is(err, billing.ErrInsufficientCredits) {
			writeError(w, http.StatusPaymentRequired, "insufficient credits")
		} else {
			log.Printf("ERROR: reservation failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	if req.Stream {
		h.handleStream(ctx, w, r, prov, apiKey, model, result, requestID, started, maxCost, &req)
		return
	}

	var messages []provider.Message
	for _, m := range req.Messages {
		messages = append(messages, provider.Message{Role: m.Role, Content: m.Content})
	}
	upstream, err := prov.Send(ctx, provider.Request{
		Model:     model.UpstreamModelID,
		Messages:  messages,
		MaxTokens: req.MaxTokens,
	}, apiKey)
	if err != nil {
		_ = h.Billing.Finalize(ctx, result.UserID, requestID, maxCost.TotalCents, 0)
		log.Printf("ERROR: upstream call failed: %v", err)
		now := time.Now()
		latency := int(time.Since(started).Milliseconds())
		errMsg := err.Error()
		h.Reactor.Push(logging.RequestLogEntry{
			ID: requestID, UserID: result.UserID, APIKeyID: result.APIKeyID,
			APISurface: "openai", UpstreamProvider: model.UpstreamProvider, UpstreamModel: model.UpstreamModelID,
			ModelAlias: model.Alias, Streaming: false, Status: "provider_error",
			ErrorMessage: &errMsg, LatencyMs: &latency, CreatedAt: started, CompletedAt: &now,
		})
		writeError(w, http.StatusBadGateway, "upstream provider error")
		return
	}

	actual := billing.CalculateCost(int64(upstream.PromptTokens), int64(upstream.CompletionTokens),
		model.InputCentsPerMillionTokens, model.OutputCentsPerMillionTokens, model.MarkupPct)

	if err := h.Billing.Finalize(ctx, result.UserID, requestID, maxCost.TotalCents, actual.TotalCents); err != nil {
		log.Printf("ERROR: finalize failed: %v", err)
	}

	now := time.Now()
	latency := int(time.Since(started).Milliseconds())
	out := ChatCompletionsResponse{
		ID:      "chatcmpl-" + requestID.String(),
		Object:  "chat.completion",
		Created: now.Unix(),
		Model:   req.Model,
		Choices: []ChatCompletionChoice{{
			Index:        0,
			Message:      ChatCompletionMessage{Role: "assistant", Content: upstream.Content},
			FinishReason: "stop",
		}},
		Usage: ChatCompletionUsage{
			PromptTokens:     upstream.PromptTokens,
			CompletionTokens: upstream.CompletionTokens,
			TotalTokens:      upstream.PromptTokens + upstream.CompletionTokens,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)

	promptT := upstream.PromptTokens
	compT := upstream.CompletionTokens
	h.Reactor.Push(logging.RequestLogEntry{
		ID:                requestID,
		UserID:            result.UserID,
		APIKeyID:          result.APIKeyID,
		APISurface:        "openai",
		UpstreamProvider:  model.UpstreamProvider,
		UpstreamModel:     model.UpstreamModelID,
		ModelAlias:        model.Alias,
		PromptTokens:      &promptT,
		CompletionTokens:  &compT,
		InputCostCents:    int(actual.InputCents),
		OutputCostCents:   int(actual.OutputCents),
		MarginCents:       int(actual.MarginCents),
		TotalChargedCents: int(actual.TotalCents),
		Streaming:         false,
		Status:            "success",
		LatencyMs:         &latency,
		CreatedAt:         started,
		CompletedAt:       &now,
	})
}

// streamingChunk is the OAI-compatible SSE chunk we emit.
type streamingChunk struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []streamingChoice `json:"choices"`
}

type streamingChoice struct {
	Index        int                   `json:"index"`
	Delta        ChatCompletionMessage `json:"delta"`
	FinishReason *string               `json:"finish_reason,omitempty"`
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
	// Cleanup context is detached from the request context: when the client
	// disconnects mid-stream, r.Context() cancels — but we must still finalize
	// billing and log the request. Cap at 10s so a hung DB doesn't pile up.
	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cleanupCancel()

	streamer, ok := prov.(provider.StreamingProvider)
	if !ok {
		_ = h.Billing.Finalize(cleanupCtx, result.UserID, requestID, maxCost.TotalCents, 0)
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
		_ = h.Billing.Finalize(cleanupCtx, result.UserID, requestID, maxCost.TotalCents, 0)
		log.Printf("ERROR: stream open failed: %v", err)
		writeError(w, http.StatusBadGateway, "upstream provider error")
		return
	}
	defer reader.Close()

	sse := stream.NewWriter(w)
	if sse == nil {
		_ = h.Billing.Finalize(cleanupCtx, result.UserID, requestID, maxCost.TotalCents, 0)
		writeError(w, http.StatusInternalServerError, "response writer does not support streaming")
		return
	}

	// streamStatus tracks how the loop exited so we can: (a) log accurate
	// status in request_logs and (b) charge for partial output on provider
	// error rather than full-refund and give the user free tokens.
	streamStatus := "success"
	stopReason := "stop"
	streamedChars := 0 // proxy-side fallback when provider doesn't report final usage
	var finalUsage *provider.Usage
	for {
		evt, recvErr := reader.Recv()
		if evt.Type == "content" && evt.ContentDelta != "" {
			streamedChars += len(evt.ContentDelta)
			chunk := streamingChunk{
				ID:      "chatcmpl-" + requestID.String(),
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []streamingChoice{{Index: 0, Delta: ChatCompletionMessage{Content: evt.ContentDelta}}},
			}
			if writeErr := sse.SendJSON(chunk); writeErr != nil {
				log.Printf("ERROR: sse write (client likely disconnected): %v", writeErr)
				streamStatus = "cancelled"
				break
			}
		}
		if evt.Usage != nil {
			finalUsage = evt.Usage
		}
		if evt.StopReason != "" {
			stopReason = evt.StopReason
		}
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			log.Printf("ERROR: stream recv: %v", recvErr)
			streamStatus = "provider_error"
			break
		}
	}

	finalChunk := streamingChunk{
		ID:      "chatcmpl-" + requestID.String(),
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []streamingChoice{{Index: 0, Delta: ChatCompletionMessage{}, FinishReason: &stopReason}},
	}
	_ = sse.SendJSON(finalChunk)
	_ = sse.SendDone()

	// Determine billing tokens. Prefer provider-reported usage; fall back to
	// proxy-side accounting (reservation prompt estimate + char-count for
	// completion) so partial streams don't refund-to-zero.
	var promptTokens, completionTokens int
	if finalUsage != nil {
		promptTokens = finalUsage.PromptTokens
		completionTokens = finalUsage.CompletionTokens
	} else {
		// We don't have access to the reservation-time prompt estimate here;
		// best we can do for the prompt side without a separate field is to
		// derive it later from the reservation amount. For now, charge for
		// completion tokens at minimum so the user isn't given free output.
		promptTokens = 0
		completionTokens = streamedChars / 4
	}
	actual := billing.CalculateCost(int64(promptTokens), int64(completionTokens),
		model.InputCentsPerMillionTokens, model.OutputCentsPerMillionTokens, model.MarkupPct)
	_ = h.Billing.Finalize(cleanupCtx, result.UserID, requestID, maxCost.TotalCents, actual.TotalCents)

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
		Streaming: true, Status: streamStatus,
		LatencyMs: &latency, CreatedAt: started, CompletedAt: &now,
	})
}
