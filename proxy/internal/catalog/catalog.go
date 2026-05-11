package catalog

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Model struct {
	Alias                       string
	UpstreamProvider            string
	UpstreamModelID             string
	ContextWindow               int
	InputCentsPerMillionTokens  int64
	OutputCentsPerMillionTokens int64
	MarkupPct                   int
	Status                      string
}

var ErrModelNotFound = errors.New("model not found")

type Catalog struct {
	pg      *pgxpool.Pool
	mu      sync.RWMutex
	byAlias map[string]Model
	stop    chan struct{}
}

func New(pg *pgxpool.Pool) *Catalog {
	return &Catalog{pg: pg, byAlias: map[string]Model{}, stop: make(chan struct{})}
}

func (c *Catalog) Refresh(ctx context.Context) error {
	const q = `
		SELECT alias, upstream_provider, upstream_model_id, context_window,
		       input_cents_per_million_tokens, output_cents_per_million_tokens,
		       markup_pct, status
		FROM model_catalog
	`
	rows, err := c.pg.Query(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()

	next := map[string]Model{}
	for rows.Next() {
		var m Model
		if err := rows.Scan(&m.Alias, &m.UpstreamProvider, &m.UpstreamModelID, &m.ContextWindow,
			&m.InputCentsPerMillionTokens, &m.OutputCentsPerMillionTokens, &m.MarkupPct, &m.Status); err != nil {
			return err
		}
		next[m.Alias] = m
	}
	c.mu.Lock()
	c.byAlias = next
	c.mu.Unlock()
	return nil
}

func (c *Catalog) Get(alias string) (Model, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m, ok := c.byAlias[alias]
	if !ok || m.Status != "active" {
		return Model{}, ErrModelNotFound
	}
	return m, nil
}

func (c *Catalog) StartAutoRefresh(ctx context.Context, every time.Duration) {
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				_ = c.Refresh(ctx)
			case <-c.stop:
				return
			}
		}
	}()
}

func (c *Catalog) Stop() { close(c.stop) }
