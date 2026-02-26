# Fast Unli

Load balancer for fast inference APIs (Cerebras, etc.) with automatic failover, health tracking, and key rotation.

## Quick Start

```bash
# Set required env vars
export FAST_UNLI_API_KEY="your-client-facing-api-key"
export CEREBRAS_KEYS="key1,key2,key3"

# Run
export PORT=8080
go run ./cmd/server/
```

## API

### Client Endpoints (requires `FAST_UNLI_API_KEY`)

- `POST /v1/chat/completions` - Proxy to provider
- `GET /v1/models` - List available models

### Admin Endpoints (requires `ADMIN_API_KEY`)

- `GET /admin/keys` - List all keys
- `POST /admin/keys` - Add a key
- `DELETE /admin/keys/:id` - Remove a key
- `POST /admin/keys/:id/enable` - Force enable a key

### Health

- `GET /health` - Service status and key stats

## Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `FAST_UNLI_API_KEY` | Yes | - | Client-facing API key |
| `CEREBRAS_KEYS` | Yes | - | Comma-separated provider keys |
| `ADMIN_API_KEY` | No | - | Admin API key |
| `PORT` | No | 8080 | Server port |
| `DB_PATH` | No | `./fast_unli.db` | SQLite database path |
| `PROVIDER_BASE_URL` | No | `https://api.cerebras.ai/v1` | Provider API URL |

## Key States

- **healthy** - Active in rotation
- **cooldown** - Temporary pause (10 min)
- **sick** - Longer pause (30 min)
- **dead** - Terminal, manual intervention needed
- **banned** - Invalid key (401/403)

## Building

```bash
# Build binary
go build -o fast-unli ./cmd/server/

# Run tests
go test ./... -v
```

## Architecture

- **Config**: Environment-based configuration
- **Store**: SQLite persistence for key metadata
- **KeyPool**: In-memory round-robin with state machine
- **Provider**: HTTP client for Cerebras API
- **Server**: Chi router with auth middleware
