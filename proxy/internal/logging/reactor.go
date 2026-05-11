package logging

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RequestLogEntry struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	APIKeyID          uuid.UUID
	APISurface        string
	UpstreamProvider  string
	UpstreamModel     string
	ModelAlias        string
	PromptTokens      *int
	CompletionTokens  *int
	InputCostCents    int
	OutputCostCents   int
	MarginCents       int
	TotalChargedCents int
	Streaming         bool
	Status            string
	ErrorMessage      *string
	LatencyMs         *int
	CreatedAt         time.Time
	CompletedAt       *time.Time
}

type Reactor struct {
	pg       *pgxpool.Pool
	inbox    chan RequestLogEntry
	flushAt  time.Duration
	maxBatch int
}

func New(pg *pgxpool.Pool) *Reactor {
	return &Reactor{
		pg:       pg,
		inbox:    make(chan RequestLogEntry, 10_000),
		flushAt:  100 * time.Millisecond,
		maxBatch: 50,
	}
}

func (r *Reactor) Push(entry RequestLogEntry) {
	select {
	case r.inbox <- entry:
	default:
		log.Printf("WARN: log inbox full, dropping entry id=%s", entry.ID)
	}
}

func (r *Reactor) Run(ctx context.Context) {
	batch := make([]RequestLogEntry, 0, r.maxBatch)
	ticker := time.NewTicker(r.flushAt)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := r.insertBatch(ctx, batch); err != nil {
			log.Printf("WARN: failed to insert log batch (%d entries): %v", len(batch), err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			for {
				select {
				case e := <-r.inbox:
					batch = append(batch, e)
				default:
					flush()
					return
				}
			}
		case e := <-r.inbox:
			batch = append(batch, e)
			if len(batch) >= r.maxBatch {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (r *Reactor) insertBatch(ctx context.Context, entries []RequestLogEntry) error {
	tx, err := r.pg.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	const q = `
		INSERT INTO request_logs
			(id, user_id, api_key_id, api_surface, upstream_provider, upstream_model, model_alias,
			 prompt_tokens, completion_tokens, input_cost_cents, output_cost_cents, margin_cents,
			 total_charged_cents, streaming, status, error_message, latency_ms, created_at, completed_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
	`
	for _, e := range entries {
		if _, err := tx.Exec(ctx, q,
			e.ID, e.UserID, e.APIKeyID, e.APISurface, e.UpstreamProvider, e.UpstreamModel, e.ModelAlias,
			e.PromptTokens, e.CompletionTokens, e.InputCostCents, e.OutputCostCents, e.MarginCents,
			e.TotalChargedCents, e.Streaming, e.Status, e.ErrorMessage, e.LatencyMs, e.CreatedAt, e.CompletedAt,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
