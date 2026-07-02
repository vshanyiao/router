# MaaS Router — Phase 4: Admin Panel + Abuse Prevention

> Execution: subagent-driven, parallel waves. Base: `main` (Phase 0-3 merged).

**Goal:** Admin panel at `/admin/*` (gated by `users.is_admin`) for pricing config, model catalog CRUD, user management, and read-only transaction/request/audit viewers. Plus abuse prevention: Cloudflare Turnstile on signup and Redis sliding-window rate limiting in the Go proxy. Fold in the deferred M1 (config-driven top-up presets) and M2 (session-creation rate limit).

Schema prereqs (`is_admin`, `app_config`, `audit_logs`) already exist from Phase 0.

## Waves

- **W1 (foundation, inline):** `lib/admin.ts` (`requireAdmin`, `logAdminAction`), gated `/admin/layout.tsx`.
- **W2 (5 parallel agents):** overview, pricing, models, users, read-only viewers — each a page + API route.
- **W3 (abuse):** Turnstile verify on signup; Go proxy rate limiter (per-user 60/min, per-IP 100/min unauth) → 429; web rate limits on signup + topup session creation.
- **W4:** M1 (`/api/config` public endpoint + modal reads it), M2 (rate-limit session creation), verify, PR, merge.

## Key interfaces

`lib/admin.ts`:
```ts
export async function requireAdmin(): Promise<{ id: string; email: string }> // throws Response 403 if not admin
export async function logAdminAction(actorId: string, kind: string, payload: unknown, targetUserId?: string): Promise<void>
```

Admin API routes live under `/api/admin/*`, all call `requireAdmin()` first, all mutations call `logAdminAction`.

Go proxy rate limiter: `internal/ratelimit/ratelimit.go` — sliding-window via Redis INCR+EXPIRE, keyed `rl:user:{id}:{min}` and `rl:ip:{ip}:{min}`. Checked in the auth phase of both `/v1/*` handlers; 429 with `Retry-After` on breach.

## Out of scope (Phase 5+)
- Bilingual admin UI (Phase 5 i18n)
- Real-time metrics streaming (static snapshot queries fine)
- Multi-admin roles/permissions (single is_admin bool)
