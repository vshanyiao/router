# MaaS Router

Reseller MaaS platform offering frontier LLM access to mainland China users.

## Quick start

1. Copy `.env.example` to `.env` and fill in values (or use defaults for local dev)
2. `make up` — starts postgres, redis, web (Next.js), proxy (Go)
3. `make migrate` — applies database migrations
4. `make seed` — seeds initial model catalog and app config
5. Open http://localhost:3000

## Architecture

See `docs/superpowers/specs/2026-05-11-maas-router-design.md`.

## Phase 0

Email/password + GitHub signup, $1 trial credit, API key management,
single model (GPT-4o non-streaming) end-to-end.
