package billing

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInsufficientCredits = errors.New("insufficient credits")

func CeilDiv(a, b int64) int64 {
	if b == 0 {
		return 0
	}
	return (a + b - 1) / b
}

type Cost struct {
	InputCents    int64
	OutputCents   int64
	ProviderCents int64
	MarginCents   int64
	TotalCents    int64
}

func CalculateCost(promptTokens, completionTokens, inputCentsPerMillion, outputCentsPerMillion int64, markupPct int) Cost {
	input := CeilDiv(promptTokens*inputCentsPerMillion, 1_000_000)
	output := CeilDiv(completionTokens*outputCentsPerMillion, 1_000_000)
	provider := input + output
	margin := CeilDiv(provider*int64(markupPct), 100)
	return Cost{
		InputCents:    input,
		OutputCents:   output,
		ProviderCents: provider,
		MarginCents:   margin,
		TotalCents:    provider + margin,
	}
}

type Service struct {
	PG *pgxpool.Pool
}

func New(pg *pgxpool.Pool) *Service {
	return &Service{PG: pg}
}

func (s *Service) Reserve(ctx context.Context, userID, requestID uuid.UUID, amount int64) error {
	tx, err := s.PG.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var balanceAfter int64
	err = tx.QueryRow(ctx, `
		UPDATE users SET credits_cents = credits_cents - $1
		WHERE id = $2 AND credits_cents >= $1
		RETURNING credits_cents
	`, amount, userID).Scan(&balanceAfter)
	if err != nil {
		return ErrInsufficientCredits
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO credit_transactions (id, user_id, amount_cents, kind, request_id, balance_after_cents, created_at)
		VALUES (gen_random_uuid(), $1, $2, 'reservation', $3, $4, now())
	`, userID, -amount, requestID, balanceAfter)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Service) Finalize(ctx context.Context, userID, requestID uuid.UUID, reserveAmount, actualCost int64) error {
	refund := reserveAmount - actualCost
	if refund < 0 {
		refund = 0
	}

	tx, err := s.PG.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var balanceAfter int64
	err = tx.QueryRow(ctx, `
		UPDATE users SET credits_cents = credits_cents + $1
		WHERE id = $2
		RETURNING credits_cents
	`, refund, userID).Scan(&balanceAfter)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO credit_transactions (id, user_id, amount_cents, kind, request_id, balance_after_cents, created_at)
		VALUES (gen_random_uuid(), $1, $2, 'consumption', $3, $4, now())
	`, userID, -actualCost, requestID, balanceAfter-refund)
	if err != nil {
		return err
	}

	if refund > 0 {
		_, err = tx.Exec(ctx, `
			INSERT INTO credit_transactions (id, user_id, amount_cents, kind, request_id, balance_after_cents, created_at)
			VALUES (gen_random_uuid(), $1, $2, 'refund', $3, $4, now())
		`, userID, refund, requestID, balanceAfter)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}
