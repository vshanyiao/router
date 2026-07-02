package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/admin/maas-router/proxy/internal/auth"
	"github.com/admin/maas-router/proxy/internal/billing"
	"github.com/admin/maas-router/proxy/internal/catalog"
	"github.com/admin/maas-router/proxy/internal/logging"
	"github.com/admin/maas-router/proxy/internal/provider"
	"github.com/admin/maas-router/proxy/internal/provider/anthropic"
	"github.com/admin/maas-router/proxy/internal/provider/gemini"
	"github.com/admin/maas-router/proxy/internal/provider/openai"
	"github.com/admin/maas-router/proxy/internal/ratelimit"
	"github.com/admin/maas-router/proxy/internal/reaper"
	"github.com/admin/maas-router/proxy/internal/server"
	"github.com/admin/maas-router/proxy/internal/storage"
	"github.com/admin/maas-router/proxy/internal/tokenizer"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pg, err := storage.NewPostgres(ctx)
	if err != nil {
		log.Fatalf("postgres init: %v", err)
	}
	defer pg.Close()

	rdb, err := storage.NewRedis(ctx)
	if err != nil {
		log.Fatalf("redis init: %v", err)
	}
	defer rdb.Close()

	hmacSecret := os.Getenv("HMAC_SERVER_SECRET")
	if hmacSecret == "" {
		log.Fatal("HMAC_SERVER_SECRET not set")
	}

	cat := catalog.New(pg)
	if err := cat.Refresh(ctx); err != nil {
		log.Fatalf("catalog refresh: %v", err)
	}
	cat.StartAutoRefresh(ctx, 60*time.Second)

	reactor := logging.New(pg)
	reactorDone := make(chan struct{})
	go func() {
		defer close(reactorDone)
		reactor.Run(ctx)
	}()

	rp := reaper.New(pg, 60*time.Second, 5*time.Minute)
	go rp.Run(ctx)

	openaiKey := os.Getenv("OPENAI_API_KEY")
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")

	tokReg := tokenizer.NewRegistry()
	oaiTok, terr := tokenizer.NewOpenAITokenizer()
	if terr != nil {
		log.Fatalf("tokenizer init: %v", terr)
	}
	tokReg.Register("openai", oaiTok)
	tokReg.Register("anthropic", tokenizer.NewAnthropicTokenizer(anthropicKey))
	tokReg.Register("google", tokenizer.NewGeminiTokenizer(geminiKey))

	authSvc := auth.New(pg, rdb, hmacSecret)
	billSvc := billing.New(pg)
	providers := map[string]provider.Provider{
		"openai":    openai.New(),
		"anthropic": anthropic.New(),
		"google":    gemini.New(),
	}
	keys := map[string]string{
		"openai":    openaiKey,
		"anthropic": anthropicKey,
		"google":    geminiKey,
	}

	limiter := ratelimit.New(rdb)
	userRPM := 60
	if v := os.Getenv("RATE_LIMIT_USER_RPM"); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil && n > 0 {
			userRPM = n
		}
	}

	handler := &server.OpenAIHandler{
		Auth:       authSvc,
		Billing:    billSvc,
		Catalog:    cat,
		Providers:  providers,
		Tokenizers: tokReg,
		Keys:       keys,
		Reactor:    reactor,
		RateLimit:  limiter,
		UserRPM:    userRPM,
	}

	anthropicHandler := &server.AnthropicHandler{
		Auth:       authSvc,
		Billing:    billSvc,
		Catalog:    cat,
		Providers:  providers,
		Tokenizers: tokReg,
		Keys:       keys,
		Reactor:    reactor,
		RateLimit:  limiter,
		UserRPM:    userRPM,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/v1/chat/completions", handler.ChatCompletions)
	mux.HandleFunc("/v1/messages", anthropicHandler.Messages)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("proxy listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	log.Println("shutting down")

	shutdownCtx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer scancel()
	_ = srv.Shutdown(shutdownCtx)
	cancel()

	// Wait for the Reactor to finish draining its inbox before exiting so
	// in-flight request_logs aren't dropped on SIGTERM. Cap at 10s — if the
	// DB is unresponsive longer than that, accept the loss and exit.
	select {
	case <-reactorDone:
	case <-time.After(10 * time.Second):
		log.Println("warn: reactor drain timed out after 10s; some logs may be lost")
	}
}
