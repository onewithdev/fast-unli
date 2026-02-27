# Fast Unli - Load Balancer Design

## Overview
A Go-based HTTP proxy that load-balances requests across multiple fast inference API keys (Cerebras, etc.) with automatic failover, health tracking, and key rotation.

## Architecture

```
┌─────────────┐     ┌─────────────────┐     ┌─────────────┐
│   Client    │────▶│  Fast Unli API  │────▶│  Key Pool   │
│  (1 API key)│     │   (Chi Router)  │     │  (SQLite)   │
└─────────────┘     └─────────────────┘     └──────┬──────┘
                                                   │
                          ┌────────────────────────┘
                          ▼
                   ┌─────────────┐
                   │  Provider   │
                   │ Client      │
                   │  (Retryer)  │
                   └──────┬──────┘
                          │
              ┌───────────┼───────────┐
              ▼           ▼           ▼
          ┌──────┐   ┌──────┐   ┌──────┐
          │Key 1 │   │Key 2 │   │Key N │
          └──────┘   └──────┘   └──────┘
```

## Key States

| State | Description | Transitions |
|-------|-------------|-------------|
| **healthy** | Active in rotation | → cooldown (429/5xx), → sick (5 fails), → banned (401/403) |
| **cooldown** | Temporary pause (10 min) | → healthy (retry success), → sick (retry fail) |
| **sick** | Longer pause (30 min), 1 rehab chance | → healthy (retry success), → dead (retry fail) |
| **dead** | Terminal, manual intervention needed | → healthy (manual enable only) |
| **banned** | Invalid key (401/403), terminal | None |

## Components

### 1. HTTP Server (Chi)
- `POST /v1/chat/completions` - Proxy endpoint (OpenAI-compatible)
- `GET /v1/models` - List models (proxy)
- `GET /health` - Service health + key stats
- Admin routes (protected by `ADMIN_API_KEY`):
  - `GET /admin/keys` - List keys (masked)
  - `POST /admin/keys` - Add key
  - `DELETE /admin/keys/:id` - Remove key
  - `POST /admin/keys/:id/enable` - Force enable

### 2. Key Pool Manager
- In-memory rotation queue of healthy keys
- SQLite persistence for state
- Round-robin selection
- Automatic state transitions on failure

### 3. Provider Client
- Makes actual requests to configured provider (e.g., `api.cerebras.ai`)
- 3-minute timeout per request attempt
- Handles streaming (SSE) transparently
- Error classification for state transitions

### 4. SQLite Store
```sql
CREATE TABLE api_keys (
    id INTEGER PRIMARY KEY,
    provider TEXT NOT NULL DEFAULT 'cerebras',
    key_value TEXT NOT NULL,
    status TEXT CHECK(status IN ('healthy', 'cooldown', 'sick', 'dead', 'banned')),
    fail_count INTEGER DEFAULT 0,
    last_used_at DATETIME,
    last_failed_at DATETIME,
    cooldown_until DATETIME,
    total_requests INTEGER DEFAULT 0,
    total_failures INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

## Error Handling

| Provider Response | Action | Client Sees |
|-------------------|--------|-------------|
| 200 OK | Reset fail_count, return response | Streamed response |
| 429 Rate Limit | Cooldown (use `retry-after` header) | Internal retry |
| 401/403 | Ban permanently | Internal retry |
| 5xx | Cooldown 10 min | Internal retry |
| Network/Timeout | Cooldown 5 min | Internal retry |
| All keys failed | - | 503 Service Unavailable |

## Configuration

```env
# Required
FAST_UNLI_API_KEY=your-client-facing-api-key
CEREBRAS_KEYS=key1,key2,key3...  # Provider-specific keys

# Optional
PORT=8080
DB_PATH=./fast_unli.db
ADMIN_API_KEY=admin-secret
MAX_RETRY_TIMEOUT=3m
COOLDOWN_MINUTES=10
SICK_MINUTES=30

# Provider config
PROVIDER_BASE_URL=https://api.cerebras.ai/v1
```

## Deployment (Fly.io)

```toml
# fly.toml
app = "fast-unli"
primary_region = "iad"

[build]

[env]
  PORT = "8080"

[[mounts]]
  source = "fast_unli_data"
  destination = "/data"

[http_service]
  internal_port = 8080
  force_https = true
  auto_stop_machines = false
  auto_start_machines = true
```

## Flow

1. **Startup:** Load env keys → Store in SQLite → Build healthy queue
2. **Request:** Validate auth → Get next healthy key → Try request
3. **Success:** Update stats, move key to back of queue
4. **Failure:** Classify error → Update state → Try next key
5. **Timeout:** After 3 min of trying all keys, return 503

## Streaming

Uses `io.Pipe()` to stream chunks from provider to client without buffering. Key state updates only happen at response end (success/failure determined by final status, not individual chunks).

## Supported Providers

| Provider | Base URL | Notes |
|----------|----------|-------|
| Cerebras | `https://api.cerebras.ai/v1` | Recommended fast inference |
| Groq | `https://api.groq.com/openai/v1` | If they come back online |

## Migration Notes

This design evolved from a Groq-specific implementation to a provider-agnostic fast inference load balancer. The architecture supports multiple providers — just add keys with the appropriate `provider` field and configure the base URL.
