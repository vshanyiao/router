package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func HashKey(plaintext, serverSecret string) string {
	mac := hmac.New(sha256.New, []byte(serverSecret))
	mac.Write([]byte(plaintext))
	return hex.EncodeToString(mac.Sum(nil))
}

type LookupResult struct {
	APIKeyID     uuid.UUID `json:"api_key_id"`
	UserID       uuid.UUID `json:"user_id"`
	CreditsCents int64     `json:"credits_cents"`
	UserStatus   string    `json:"user_status"`
}

var (
	ErrKeyNotFound = errors.New("api key not found or revoked")
	ErrUserBanned  = errors.New("user not active")
)

const cacheTTL = 5 * time.Minute

type Auth struct {
	PG     *pgxpool.Pool
	RDB    *redis.Client
	Secret string
}

func New(pg *pgxpool.Pool, rdb *redis.Client, secret string) *Auth {
	return &Auth{PG: pg, RDB: rdb, Secret: secret}
}

func (a *Auth) LookupBearer(ctx context.Context, header string) (*LookupResult, error) {
	if !strings.HasPrefix(header, "Bearer ") {
		return nil, ErrKeyNotFound
	}
	plaintext := strings.TrimPrefix(header, "Bearer ")
	return a.Lookup(ctx, plaintext)
}

func (a *Auth) Lookup(ctx context.Context, plaintext string) (*LookupResult, error) {
	hash := HashKey(plaintext, a.Secret)

	cacheKey := "key:" + hash
	cached, err := a.RDB.Get(ctx, cacheKey).Result()
	if err == nil {
		var r LookupResult
		if jerr := json.Unmarshal([]byte(cached), &r); jerr == nil {
			if r.UserStatus != "active" {
				return nil, ErrUserBanned
			}
			return &r, nil
		}
	}

	const q = `
		SELECT k.id, k.user_id, u.credits_cents, u.status
		FROM api_keys k
		JOIN users u ON u.id = k.user_id
		WHERE k.key_hash = $1 AND k.revoked_at IS NULL
		LIMIT 1
	`
	var r LookupResult
	err = a.PG.QueryRow(ctx, q, hash).Scan(&r.APIKeyID, &r.UserID, &r.CreditsCents, &r.UserStatus)
	if err != nil {
		return nil, ErrKeyNotFound
	}
	if r.UserStatus != "active" {
		return nil, ErrUserBanned
	}

	if b, jerr := json.Marshal(&r); jerr == nil {
		_ = a.RDB.Set(ctx, cacheKey, b, cacheTTL).Err()
	}

	return &r, nil
}
