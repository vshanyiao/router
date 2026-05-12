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
echo "=== Streaming chat completion request ==="
curl -sS -N -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "anthropic/claude-haiku-4-5",
    "messages": [{"role":"user","content":"Count from 1 to 5, one per line."}],
    "max_tokens": 40,
    "stream": true
  }' | head -30

echo
echo "=== Anthropic surface + Anthropic provider (native, non-streaming) ==="
curl -sS -X POST http://localhost:8080/v1/messages \
  -H "x-api-key: $API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "anthropic/claude-haiku-4-5",
    "max_tokens": 40,
    "messages": [{"role":"user","content":"Say hi in two words."}]
  }' | jq '{stop_reason, content: .content[0].text}'

echo
echo "=== Anthropic surface + OAI provider (cross-format) ==="
curl -sS -X POST http://localhost:8080/v1/messages \
  -H "x-api-key: $API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "openai/gpt-4o-mini",
    "max_tokens": 40,
    "messages": [{"role":"user","content":"Say hi in two words."}]
  }' | jq '{stop_reason, content: .content[0].text}'

echo
echo "=== Anthropic surface + Gemini provider (cross-format) ==="
curl -sS -X POST http://localhost:8080/v1/messages \
  -H "x-api-key: $API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "google/gemini-2.5-flash",
    "max_tokens": 40,
    "messages": [{"role":"user","content":"Say hi in two words."}]
  }' | jq '{stop_reason, content: .content[0].text}'

echo
echo "=== OAI surface + Anthropic provider tool call ==="
curl -sS -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "anthropic/claude-haiku-4-5",
    "messages": [{"role":"user","content":"What is the weather in Paris?"}],
    "tools": [{
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "Get current weather for a city",
        "parameters": {"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}
      }
    }],
    "max_tokens": 200
  }' | jq '.choices[0].message.tool_calls[0] // .choices[0].message.content'

echo
echo "=== OAI surface + Gemini vision ==="
curl -sS -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "google/gemini-2.5-flash",
    "messages": [{"role":"user","content":[
      {"type":"text","text":"What color is this image? Answer in one word."},
      {"type":"image_url","image_url":{"url":"https://upload.wikimedia.org/wikipedia/commons/thumb/4/4f/Red_square.png/120px-Red_square.png"}}
    ]}],
    "max_tokens": 30
  }' | jq '.choices[0].message.content'

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
