package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/admin/maas-router/proxy/internal/auth"
	"github.com/admin/maas-router/proxy/internal/billing"
	"github.com/admin/maas-router/proxy/internal/catalog"
	"github.com/admin/maas-router/proxy/internal/logging"
	"github.com/admin/maas-router/proxy/internal/provider"
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
	Auth      *auth.Auth
	Billing   *billing.Service
	Catalog   *catalog.Catalog
	OpenAI    provider.Provider
	OpenAIKey string
	Reactor   *logging.Reactor
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
	if req.Stream {
		writeError(w, http.StatusBadRequest, "streaming not yet supported (Phase 0 limitation)")
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
	if model.UpstreamProvider != "openai" {
		writeError(w, http.StatusBadRequest, "Phase 0 supports OpenAI models only")
		return
	}

	requestID := uuid.New()
	var promptChars int
	for _, m := range req.Messages {
		promptChars += len(m.Content)
	}
	estPromptTokens := int64(promptChars / 4)
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

	var messages []provider.Message
	for _, m := range req.Messages {
		messages = append(messages, provider.Message{Role: m.Role, Content: m.Content})
	}
	upstream, err := h.OpenAI.Send(ctx, provider.Request{
		Model:     model.UpstreamModelID,
		Messages:  messages,
		MaxTokens: req.MaxTokens,
	}, h.OpenAIKey)
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
