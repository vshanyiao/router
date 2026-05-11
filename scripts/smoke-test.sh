#!/usr/bin/env bash
set -euo pipefail

# Reads $API_KEY from environment. Pass the plaintext key created in /dashboard/keys.

if [ -z "${API_KEY:-}" ]; then
  echo "Usage: API_KEY=sk-or-... ./scripts/smoke-test.sh"
  exit 1
fi

echo "=== Checking proxy health ==="
curl -fsS http://localhost:8080/healthz && echo

echo
echo "=== Making chat completion request ==="
curl -sS -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "openai/gpt-4o",
    "messages": [{"role": "user", "content": "Say hello in one word."}],
    "max_tokens": 20
  }' | tee /tmp/maas-response.json

echo
echo
echo "=== Response details ==="
echo "Status: $(jq -r '.choices[0].finish_reason' /tmp/maas-response.json)"
echo "Tokens: $(jq -r '.usage.prompt_tokens' /tmp/maas-response.json) in / $(jq -r '.usage.completion_tokens' /tmp/maas-response.json) out"

echo
echo "=== Asking DB for the request log (after 1s drain) ==="
sleep 1
docker compose exec -T postgres psql -U app -d maas -c "
  SELECT model_alias, status, prompt_tokens, completion_tokens, total_charged_cents, latency_ms
  FROM request_logs ORDER BY created_at DESC LIMIT 1;
"

echo
echo "=== Current balance ==="
docker compose exec -T postgres psql -U app -d maas -c "
  SELECT email, credits_cents FROM users
  WHERE id = (SELECT user_id FROM request_logs ORDER BY created_at DESC LIMIT 1);
"
