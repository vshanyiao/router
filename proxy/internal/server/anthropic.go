package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/admin/maas-router/proxy/internal/auth"
	"github.com/admin/maas-router/proxy/internal/billing"
	"github.com/admin/maas-router/proxy/internal/canonical"
	"github.com/admin/maas-router/proxy/internal/catalog"
	"github.com/admin/maas-router/proxy/internal/logging"
	"github.com/admin/maas-router/proxy/internal/provider"
	"github.com/admin/maas-router/proxy/internal/stream"
	"github.com/admin/maas-router/proxy/internal/tokenizer"
	tanthropic "github.com/admin/maas-router/proxy/internal/translate/anthropic"
)

// AnthropicHandler serves POST /v1/messages, the Anthropic-compatible surface.
// Shares Auth/Billing/Catalog/Reactor with the OAI handler — only the wire
// format differs.
type AnthropicHandler struct {
	Auth       *auth.Auth
	Billing    *billing.Service
	Catalog    *catalog.Catalog
	Providers  map[string]provider.Provider
	Tokenizers *tokenizer.Registry
	Keys       map[string]string
	Reactor    *logging.Reactor
}

// extractBearer accepts either Anthropic's `x-api-key: sk-or-...` or the
// `Authorization: Bearer sk-or-...` header that most clients also support.
func extractBearer(r *http.Request) string {
	if v := r.Header.Get("x-api-key"); v != "" {
		return "Bearer " + v
	}
	return r.Header.Get("Authorization")
}

func writeAnthropicError(w http.ResponseWriter, status int, errType, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":  "error",
		"error": map[string]string{"type": errType, "message": msg},
	})
}

func (h *AnthropicHandler) Messages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	started := time.Now()

	// Cleanup context survives client disconnect for billing finalize + log.
	cleanupCtx, cleanupCancel := contextWithoutCancel(ctx, 10*time.Second)
	defer cleanupCancel()

	var req tanthropic.MessagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON body")
		return
	}
	if req.Model == "" {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if len(req.Messages) == 0 {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "messages required")
		return
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 1024
	}

	// Lower to canonical IR
	canReq, err := tanthropic.ToCanonical(req)
	if err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	// Auth
	result, err := h.Auth.LookupBearer(ctx, extractBearer(r))
	if err != nil {
		if errors.Is(err, auth.ErrUserBanned) {
			writeAnthropicError(w, http.StatusForbidden, "permission_error", "account suspended or banned")
		} else {
			writeAnthropicError(w, http.StatusUnauthorized, "authentication_error", "invalid API key")
		}
		return
	}

	// Catalog lookup — use req.Model as alias; map to upstream
	model, err := h.Catalog.Get(req.Model)
	if err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "model '"+req.Model+"' not found")
		return
	}

	// Resolve provider + key
	prov, provOK := h.Providers[model.UpstreamProvider]
	if !provOK {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "no provider adapter for "+model.UpstreamProvider)
		return
	}
	apiKey, keyOK := h.Keys[model.UpstreamProvider]
	if !keyOK || apiKey == "" {
		writeAnthropicError(w, http.StatusInternalServerError, "api_error", "provider key not configured")
		return
	}
	canReq.Model = model.UpstreamModelID

	canProv, isCanonical := prov.(provider.CanonicalProvider)
	if !isCanonical {
		writeAnthropicError(w, http.StatusInternalServerError, "api_error", "provider does not support canonical IR")
		return
	}

	// Tokenize prompt for cost reservation
	requestID := uuid.New()
	tk, err := h.Tokenizers.Get(model.UpstreamProvider)
	if err != nil {
		log.Printf("ERROR: no tokenizer for provider %q: %v", model.UpstreamProvider, err)
		writeAnthropicError(w, http.StatusInternalServerError, "api_error", "internal error")
		return
	}
	estPromptTokens64, terr := tk.CountPromptTokens(ctx, model.UpstreamModelID, canonicalMessagesToTokenizerMessages(canReq))
	if terr != nil {
		estPromptTokens64 = canonicalCharCount(canReq) / 4
	}
	maxCost := billing.CalculateCost(int64(estPromptTokens64), int64(req.MaxTokens),
		model.InputCentsPerMillionTokens, model.OutputCentsPerMillionTokens, model.MarkupPct)
	if err := h.Billing.Reserve(ctx, result.UserID, requestID, maxCost.TotalCents); err != nil {
		if errors.Is(err, billing.ErrInsufficientCredits) {
			writeAnthropicError(w, http.StatusPaymentRequired, "billing_error", "insufficient credits")
		} else {
			log.Printf("ERROR: reservation failed: %v", err)
			writeAnthropicError(w, http.StatusInternalServerError, "api_error", "internal error")
		}
		return
	}

	if req.Stream {
		h.handleStream(ctx, cleanupCtx, w, canProv, apiKey, model, result, requestID, started, maxCost, canReq)
		return
	}

	resp, err := canProv.SendCanonical(ctx, canReq, apiKey)
	if err != nil {
		_ = h.Billing.Finalize(cleanupCtx, result.UserID, requestID, maxCost.TotalCents, 0)
		log.Printf("ERROR: upstream call failed: %v", err)
		now := time.Now()
		latency := int(time.Since(started).Milliseconds())
		errMsg := err.Error()
		h.Reactor.Push(logging.RequestLogEntry{
			ID: requestID, UserID: result.UserID, APIKeyID: result.APIKeyID,
			APISurface: "anthropic", UpstreamProvider: model.UpstreamProvider, UpstreamModel: model.UpstreamModelID,
			ModelAlias: model.Alias, Streaming: false, Status: "provider_error",
			ErrorMessage: &errMsg, LatencyMs: &latency, CreatedAt: started, CompletedAt: &now,
		})
		writeAnthropicError(w, http.StatusBadGateway, "api_error", "upstream provider error")
		return
	}

	actual := billing.CalculateCost(int64(resp.Usage.PromptTokens), int64(resp.Usage.CompletionTokens),
		model.InputCentsPerMillionTokens, model.OutputCentsPerMillionTokens, model.MarkupPct)
	_ = h.Billing.Finalize(cleanupCtx, result.UserID, requestID, maxCost.TotalCents, actual.TotalCents)

	// Build Anthropic response
	out := tanthropic.FromCanonical("msg_"+requestID.String(), model.Alias, *resp)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)

	now := time.Now()
	latency := int(time.Since(started).Milliseconds())
	pt, ct := resp.Usage.PromptTokens, resp.Usage.CompletionTokens
	h.Reactor.Push(logging.RequestLogEntry{
		ID:                requestID,
		UserID:            result.UserID,
		APIKeyID:          result.APIKeyID,
		APISurface:        "anthropic",
		UpstreamProvider:  model.UpstreamProvider,
		UpstreamModel:     model.UpstreamModelID,
		ModelAlias:        model.Alias,
		PromptTokens:      &pt,
		CompletionTokens:  &ct,
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

func (h *AnthropicHandler) handleStream(
	ctx, cleanupCtx context.Context,
	w http.ResponseWriter,
	prov provider.CanonicalProvider,
	apiKey string,
	model catalog.Model,
	result *auth.LookupResult,
	requestID uuid.UUID,
	started time.Time,
	maxCost billing.Cost,
	req canonical.Request,
) {
	reader, err := prov.SendCanonicalStream(ctx, req, apiKey)
	if err != nil {
		_ = h.Billing.Finalize(cleanupCtx, result.UserID, requestID, maxCost.TotalCents, 0)
		log.Printf("ERROR: stream open failed: %v", err)
		writeAnthropicError(w, http.StatusBadGateway, "api_error", "upstream provider error")
		return
	}
	defer reader.Close()

	sse := stream.NewWriter(w)
	if sse == nil {
		_ = h.Billing.Finalize(cleanupCtx, result.UserID, requestID, maxCost.TotalCents, 0)
		writeAnthropicError(w, http.StatusInternalServerError, "api_error", "response writer does not support streaming")
		return
	}

	mapper := tanthropic.NewStreamMapper("msg_"+requestID.String(), model.Alias)

	// We need to send message_start with prompt_tokens — wait for the first
	// usage signal from the stream or for the StreamEventStop. To avoid
	// blocking message_start unnecessarily, send it eagerly with 0 prompt
	// tokens (Anthropic's clients accept this; the real count arrives in
	// message_delta).
	for _, fe := range mapper.Start(0) {
		_ = sse.SendFrame(fe.Event, fe.Data)
	}

	streamStatus := "success"
	streamedChars := 0
	var finalUsage *canonical.Usage
	for {
		evt, recvErr := reader.Recv()
		if evt.Type == canonical.StreamEventContent && evt.ContentDelta != "" {
			streamedChars += len(evt.ContentDelta)
		}
		if evt.Usage != nil {
			finalUsage = evt.Usage
		}
		for _, fe := range mapper.Map(evt) {
			if err := sse.SendFrame(fe.Event, fe.Data); err != nil {
				log.Printf("ERROR: sse write: %v", err)
				streamStatus = "cancelled"
				goto streamDone
			}
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
streamDone:

	var promptTokens, completionTokens int
	if finalUsage != nil {
		promptTokens = finalUsage.PromptTokens
		completionTokens = finalUsage.CompletionTokens
	} else {
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
		APISurface: "anthropic", UpstreamProvider: model.UpstreamProvider,
		UpstreamModel: model.UpstreamModelID, ModelAlias: model.Alias,
		PromptTokens: &pt, CompletionTokens: &ct,
		InputCostCents: int(actual.InputCents), OutputCostCents: int(actual.OutputCents),
		MarginCents: int(actual.MarginCents), TotalChargedCents: int(actual.TotalCents),
		Streaming: true, Status: streamStatus,
		LatencyMs: &latency, CreatedAt: started, CompletedAt: &now,
	})
}

// canonicalMessagesToTokenizerMessages flattens canonical messages into the
// flat (role, text) shape that the tokenizer interface expects. Tool calls and
// images are best-effort represented as text for prompt-cost estimation.
func canonicalMessagesToTokenizerMessages(req canonical.Request) []tokenizer.Message {
	var out []tokenizer.Message
	if req.System != "" {
		out = append(out, tokenizer.Message{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		var text strings.Builder
		for _, b := range m.Content {
			switch b.Type {
			case canonical.BlockText:
				text.WriteString(b.Text)
			case canonical.BlockToolUse:
				text.WriteString(b.ToolName)
				text.WriteString(" ")
				text.Write(b.ToolInput)
			case canonical.BlockToolResult:
				text.WriteString(b.ToolResult)
			case canonical.BlockImage:
				// Tokenizers don't price image tokens well at the prompt level;
				// 256 bytes is a heuristic placeholder.
				text.WriteString("[image]")
			}
		}
		out = append(out, tokenizer.Message{Role: string(m.Role), Content: text.String()})
	}
	return out
}

func canonicalCharCount(req canonical.Request) int {
	n := len(req.System)
	for _, m := range req.Messages {
		for _, b := range m.Content {
			n += len(b.Text) + len(b.ToolResult) + len(b.ToolName) + len(b.ToolInput)
		}
	}
	return n
}
