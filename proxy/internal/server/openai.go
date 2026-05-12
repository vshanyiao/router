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
	"github.com/admin/maas-router/proxy/internal/canonical"
	"github.com/admin/maas-router/proxy/internal/catalog"
	"github.com/admin/maas-router/proxy/internal/logging"
	"github.com/admin/maas-router/proxy/internal/provider"
	"github.com/admin/maas-router/proxy/internal/stream"
	"github.com/admin/maas-router/proxy/internal/tokenizer"
	toai "github.com/admin/maas-router/proxy/internal/translate/oai"
)

// OpenAIHandler serves POST /v1/chat/completions, the OpenAI-compatible
// surface. Lowering goes through translate/oai → canonical → provider, so
// tool calls and vision work across all three upstream providers.
type OpenAIHandler struct {
	Auth       *auth.Auth
	Billing    *billing.Service
	Catalog    *catalog.Catalog
	Providers  map[string]provider.Provider
	Tokenizers *tokenizer.Registry
	Keys       map[string]string
	Reactor    *logging.Reactor
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"message": msg, "type": "invalid_request_error"},
	})
}

func (h *OpenAIHandler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	started := time.Now()

	cleanupCtx, cleanupCancel := contextWithoutCancel(ctx, 10*time.Second)
	defer cleanupCancel()

	var req toai.ChatCompletionsRequest
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

	canReq, err := toai.ToCanonical(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
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
	canReq.Model = model.UpstreamModelID

	canProv, isCanonical := prov.(provider.CanonicalProvider)
	if !isCanonical {
		writeError(w, http.StatusInternalServerError, "provider does not support canonical IR")
		return
	}

	requestID := uuid.New()
	tk, err := h.Tokenizers.Get(model.UpstreamProvider)
	if err != nil {
		log.Printf("ERROR: no tokenizer for provider %q: %v", model.UpstreamProvider, err)
		writeError(w, http.StatusInternalServerError, "internal error")
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
			writeError(w, http.StatusPaymentRequired, "insufficient credits")
		} else {
			log.Printf("ERROR: reservation failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	if req.Stream {
		h.handleStream(ctx, cleanupCtx, w, canProv, apiKey, model, result, requestID, started, maxCost, canReq, req.Model)
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
			APISurface: "openai", UpstreamProvider: model.UpstreamProvider, UpstreamModel: model.UpstreamModelID,
			ModelAlias: model.Alias, Streaming: false, Status: "provider_error",
			ErrorMessage: &errMsg, LatencyMs: &latency, CreatedAt: started, CompletedAt: &now,
		})
		writeError(w, http.StatusBadGateway, "upstream provider error")
		return
	}

	actual := billing.CalculateCost(int64(resp.Usage.PromptTokens), int64(resp.Usage.CompletionTokens),
		model.InputCentsPerMillionTokens, model.OutputCentsPerMillionTokens, model.MarkupPct)
	_ = h.Billing.Finalize(cleanupCtx, result.UserID, requestID, maxCost.TotalCents, actual.TotalCents)

	now := time.Now()
	out := toai.FromCanonical("chatcmpl-"+requestID.String(), req.Model, now.Unix(), *resp)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)

	latency := int(time.Since(started).Milliseconds())
	pt, ct := resp.Usage.PromptTokens, resp.Usage.CompletionTokens
	h.Reactor.Push(logging.RequestLogEntry{
		ID:                requestID,
		UserID:            result.UserID,
		APIKeyID:          result.APIKeyID,
		APISurface:        "openai",
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

func (h *OpenAIHandler) handleStream(
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
	modelAlias string,
) {
	reader, err := prov.SendCanonicalStream(ctx, req, apiKey)
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

	mapper := toai.NewStreamMapper("chatcmpl-"+requestID.String(), modelAlias, time.Now().Unix())

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
		if chunk := mapper.Map(evt); chunk != nil {
			if err := sse.SendJSON(chunk); err != nil {
				log.Printf("ERROR: sse write: %v", err)
				streamStatus = "cancelled"
				break
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

	_ = sse.SendDone()

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
		APISurface: "openai", UpstreamProvider: model.UpstreamProvider,
		UpstreamModel: model.UpstreamModelID, ModelAlias: model.Alias,
		PromptTokens: &pt, CompletionTokens: &ct,
		InputCostCents: int(actual.InputCents), OutputCostCents: int(actual.OutputCents),
		MarginCents: int(actual.MarginCents), TotalChargedCents: int(actual.TotalCents),
		Streaming: true, Status: streamStatus,
		LatencyMs: &latency, CreatedAt: started, CompletedAt: &now,
	})
}
