package reaper

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Reaper sweeps stuck reservations (rows in credit_transactions kind='reservation'
// older than threshold) and refunds them.
type Reaper struct {
	pg        *pgxpool.Pool
	tick      time.Duration
	threshold time.Duration
}

func New(pg *pgxpool.Pool, tick, threshold time.Duration) *Reaper {
	return &Reaper{pg: pg, tick: tick, threshold: threshold}
}

func (r *Reaper) Run(ctx context.Context) {
	t := time.NewTicker(r.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.sweep(ctx)
		}
	}
}

func (r *Reaper) sweep(ctx context.Context) {
	const findQ = `
		SELECT id, user_id, request_id, -amount_cents AS refund_amount
		FROM credit_transactions
		WHERE kind = 'reservation'
		  AND created_at < now() - $1::interval
		  AND request_id NOT IN (
			SELECT request_id FROM credit_transactions
			WHERE kind IN ('consumption','refund')
			  AND request_id IS NOT NULL
		  )
		LIMIT 100
	`
	thresh := r.threshold.String()
	rows, err := r.pg.Query(ctx, findQ, thresh)
	if err != nil {
		log.Printf("reaper sweep: %v", err)
		return
	}
	type stuck struct {
		ID, UserID, RequestID string
		RefundAmount          int64
	}
	var stucks []stuck
	for rows.Next() {
		var s stuck
		if err := rows.Scan(&s.ID, &s.UserID, &s.RequestID, &s.RefundAmount); err == nil {
			stucks = append(stucks, s)
		}
	}
	rows.Close()

	for _, s := range stucks {
		if err := r.refund(ctx, s.UserID, s.RequestID, s.RefundAmount); err != nil {
			log.Printf("reaper: refund failed for user %s req %s: %v", s.UserID, s.RequestID, err)
			continue
		}
		log.Printf("reaper: refunded %d cents to user %s for stuck request %s", s.RefundAmount, s.UserID, s.RequestID)
	}
}

// refund performs the balance restoration and audit row insertion in a single
// transaction. Without this, a crash between the UPDATE and the INSERT could
// leave the balance restored with no ledger entry — a phantom credit that the
// next sweep would refund again (double-credit).
func (r *Reaper) refund(ctx context.Context, userID, requestID string, amount int64) error {
	tx, err := r.pg.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var balanceAfter int64
	if err := tx.QueryRow(ctx, `
		UPDATE users SET credits_cents = credits_cents + $1
		WHERE id = $2
		RETURNING credits_cents
	`, amount, userID).Scan(&balanceAfter); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO credit_transactions
			(id, user_id, amount_cents, kind, request_id, balance_after_cents, description, created_at)
		VALUES (gen_random_uuid(), $1, $2, 'refund', $3, $4, 'reaper: stuck reservation', now())
	`, userID, amount, requestID, balanceAfter); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
