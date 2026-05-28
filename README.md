# Payment Orchestrator

A payment orchestrator service that routes payouts through multiple providers (Stripe, mock providers) with automatic fallback support.

## Architecture

```
                                    ┌─────────────────────────────────────────────────────────────┐
                                    │                        CLIENT                                │
                                    │                   (Merchant System)                         │
                                    └─────────────────────────────┬───────────────────────────────┘
                                                                  │
                                                                  │ HTTP POST /v1/payouts
                                                                  ▼
                                    ┌─────────────────────────────────────────────────────────────┐
                                    │                        API (:8080)                          │
                                    │                                                             │
                                    │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
                                    │  │ Rate Limiter│  │ HTTP Router │  │ Prometheus Metrics  │  │
                                    │  │  (Redis)    │  │   (Chi)     │  │    (/metrics)       │  │
                                    │  └─────────────┘  └──────┬──────┘  └─────────────────────┘  │
                                    └──────────────────────────┼──────────────────────────────────┘
                                                               │
                                                               │ gRPC (CreatePayout, GetPayout, CancelPayout)
                                                               ▼
                                    ┌─────────────────────────────────────────────────────────────┐
                                    │                    ORCHESTRATOR (:50051)                    │
                                    │                                                             │
                                    │  ┌──────────────────────────────────────────────────────┐  │
                                    │  │                    Routing Layer                      │  │
                                    │  │  ┌──────────┐  ┌──────────┐  ┌─────────────────────┐  │  │
                                    │  │  │ Priority │  │ Weighted │  │ SuccessBased        │  │  │
                                    │  │  │          │  │          │  │ (track success %)   │  │  │
                                    │  │  └──────────┘  └──────────┘  └─────────────────────┘  │  │
                                    │  └──────────────────────────┬───────────────────────────┘  │
                                    │                             │                              │
                                    │  ┌──────────────────────────▼───────────────────────────┐  │
                                    │  │              Fallback Engine                         │  │
                                    │  │   Provider A ──► (fail) ──► Provider B ──► (fail)    │  │
                                    │  │                                                       │  │
                                    │  │   Terminal errors (decline, fraud) stop immediately  │  │
                                    │  └──────────────────────────────────────────────────────┘  │
                                    └──────────────────────────┬──────────────────────────────────┘
                                                               │
                                    ┌──────────────────────────┼──────────────────────────────────┐
                                    │                          ▼                                  │
                                    │  ┌─────────────────────────────────────────────────────┐   │
                                    │  │                   PROVIDERS                          │   │
                                    │  │                                                      │   │
                                    │  │   ┌─────────┐   ┌─────────┐   ┌─────────────┐        │   │
                                    │  │   │ Stripe  │   │Mock Card│   │ Crypto Sim  │        │   │
                                    │  │   │ (card)  │   │ (card)  │   │  (crypto)   │        │   │
                                    │  │   └─────────┘   └─────────┘   └─────────────┘        │   │
                                    │  │        │              │               │              │   │
                                    │  │        └──────────────┴───────────────┘              │   │
                                    │  │                       │                              │   │
                                    │  │                       ▼                              │   │
                                    │  │              External Payment Rails                  │   │
                                    │  └─────────────────────────────────────────────────────┘   │
                                    └──────────────────────────────────────────────────────────────┘

                                    ┌─────────────────────────────────────────────────────────────┐
                                    │                    INFRASTRUCTURE                            │
                                    │                                                             │
                                    │  ┌─────────────┐  ┌─────────────┐                            │
                                    │  │  PostgreSQL │  │    Redis    │                            │
                                    │  │  (payouts)  │  │(rate limit) │                            │
                                    │  └─────────────┘  └─────────────┘                            │
                                    │                                                             │
                                    │  ┌─────────────────────────────────────────────────────────┐│
                                    │  │                  OBSERVABILITY                          ││
                                    │  │                                                         ││
                                    │  │  ┌───────────────┐  ┌───────────────┐  ┌─────────────┐  ││
                                    │  │  │VictoriaMetrics│  │ VictoriaLogs  │  │   Grafana   │  ││
                                    │  │  │   :8428       │  │    :9428      │  │   :3000     │  ││
                                    │  │  │  (metrics)    │  │   (logs)      │  │ (dashboards)│  ││
                                    │  │  └───────▲───────┘  └───────▲───────┘  └──────▲──────┘  ││
                                    │  │          │                  │                 │         ││
                                    │  │  ┌───────┴───────┐  ┌───────┴───────┐          │         ││
                                    │  │  │   vmagent     │  │    Vector     │          │         ││
                                    │  │  │(metrics scrape)│ │(log collector)│          │         ││
                                    │  │  └───────────────┘  └───────────────┘          │         ││
                                    │  └─────────────────────────────────────────────────│─────────┘│
                                    └─────────────────────────────────────────────────────┘─────────┘
```

## Features

- **Multi-provider routing** - Route payouts through Stripe, mock providers, or crypto simulator
- **Automatic fallback** - On retryable errors, automatically try the next provider
- **Routing algorithms** - Priority, Weighted, and Success-based routing
- **Idempotent API** - Safe request retries with idempotency keys
- **Rate limiting** - Redis-backed token bucket rate limiter
- **Webhook processing** - Provider webhook handling with signature verification and deduplication (Stripe implemented, extensible for other providers)
- **Observability** - VictoriaMetrics metrics/logs + Grafana dashboards

## Quick Start

```bash
# Start all services in Docker (API + Postgres + Redis + Observability)
make services-up

# Run migrations (first time only)
make migrate-up

# Services are now running:
# - API: http://localhost:8080
# - Grafana: http://localhost:3000 (admin/admin)
# - VictoriaMetrics: http://localhost:8428
# - VictoriaLogs: http://localhost:9428
```

For local development without Docker:

```bash
# Start just infrastructure
make up

# Run migrations
make migrate-up

# Build services
make build

# Run orchestrator and API locally
./bin/orchestrator start --config deploy/configs/orchestrator-local.yaml &
./bin/api &
```

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/v1/payouts` | Create a new payout |
| `GET` | `/v1/payouts/{id}` | Get payout status |
| `POST` | `/v1/payouts/{id}/cancel` | Cancel a pending payout |
| `GET` | `/health` | Health check |
| `GET` | `/metrics` | Prometheus metrics |

Full API documentation: [docs/openapi.yaml](docs/openapi.yaml)

## Example Request

```bash
curl -X POST http://localhost:8080/v1/payouts \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{"amount": 1000, "currency": "USD", "rail": "card"}'
```

Response:
```json
{"payout_id": "550e8400-e29b-41d4-a716-446655440000"}
```

## Configuration

Copy `.env.example` to `.env` and configure:

```env
API_ENV=development
API_HTTP_ADDR=:8080
API_RATE_LIMIT_ENABLED=true
```

## Observability

The project uses the **VictoriaMetrics ecosystem** for observability:

| Component | Port | Purpose |
|-----------|------|---------|
| VictoriaMetrics | 8428 | Metrics storage (Prometheus-compatible) |
| VictoriaLogs | 9428 | Log storage |
| vmagent | - | Metrics scraper (scrapes `/metrics` from services) |
| Vector | - | Log collector (reads Docker container logs) |
| Grafana | 3000 | Visualization (admin/admin) |

### Starting Observability Stack

```bash
# Start all services in Docker (API + dependencies + observability)
make services-up

# Or start just observability stack
make observability-up

# Open Grafana
make grafana-open

# Open VictoriaMetrics UI
make vm-open

# Open VictoriaLogs UI
make vlogs-open
```

### Querying Logs in Grafana

1. Go to **Explore** → Select **VictoriaLogs** datasource
2. Use LogsQL syntax:

```
service:fintech-api                    # API logs only
service:~"fintech-.*"                  # All project services
service:fintech-api _msg:~"error"      # API logs containing "error"
```

### Querying Metrics

VictoriaMetrics is compatible with PromQL. In Grafana Explore:

```promql
rate(api_http_request_duration_seconds_count[5m])  # Request rate
histogram_quantile(0.95, rate(api_http_request_duration_seconds_bucket[5m]))  # P95 latency
```

Pre-built dashboards:
- **API Overview** - HTTP latency, request rate, rate limiter stats
- **Orchestrator Overview** - gRPC latency, provider distribution

## Load Testing

```bash
# Run k6 load tests
make loadtest-create      # Create payout load test
make loadtest-rate-limit  # Rate limiter stress test
```

## Development

```bash
make build    # Build all services
make test     # Run tests
make lint     # Run linter
```

## Architecture Details

### Payout Flow

1. **CreatePayout** request received with `idempotency_key` and `request_hash`
2. Orchestrator selects providers based on configured routing algorithm
3. First provider is tried; on failure, fallback to next provider
4. Payout state transitions: `pending` → `sent` → `completed`/`failed`
5. Webhooks from providers update payout status via `ApplyProviderUpdate`

### Fallback Behavior

When a provider fails with a **retryable error** (network timeout, 5xx), the orchestrator automatically tries the next provider in the ordered list:

```
Provider A (fail) → Provider B (fail) → Provider C (success)
```

**Terminal errors** (card decline, fraud, invalid request) stop fallback immediately - no point trying another provider.

### Error Classification

| Error Type | Retryable | Fallback |
|------------|-----------|----------|
| Network timeout | ✅ | Yes |
| 5xx server error | ✅ | Yes |
| 429 rate limit | ✅ | Yes |
| Card declined | ❌ | No |
| Fraud detected | ❌ | No |
| Invalid API key | ❌ | No |
| Invalid request | ❌ | No |

### Idempotency

Every `CreatePayout` requires:
- `idempotency_key` - Unique request identifier
- `request_hash` - SHA-256 hash of canonical request body

Behavior:
- Same key + same hash → Returns existing payout
- Same key + different hash → `409 AlreadyExists` error

### Webhook Processing

The webhook service handles provider callbacks with:
- **Signature verification** - Cryptographic validation of webhook signatures
- **Event deduplication** - Redis-backed SETNX prevents duplicate processing (24h TTL)
- **Event normalization** - Provider-specific events mapped to domain types

Currently supports **Stripe** with an extensible architecture for adding new providers. To add a new provider:
1. Implement the `EventParser` interface in `services/webhook/internal/`
2. Add provider-specific event type mappings
3. Register the handler in the router

### Webhook Deduplication

Events are deduplicated using Redis SETNX with 24h TTL:
- Prevents reprocessing the same event multiple times
- Key format: `webhook:event:{provider}:{event_id}`

## Testing Provider Integration

### Stripe Example

```bash
# 1. Start Stripe CLI to forward webhooks
stripe listen --forward-to localhost:8082/v1/webhooks/stripe

# 2. Create a payout
grpcurl -plaintext -d '{
  "idempotency_key": "test-'$(date +%s)'",
  "request_hash": "hash-'$(date +%s)'",
  "amount": 1500,
  "currency": "usd",
  "rail": "card"
}' localhost:50051 orchestrator.v1.PayoutService/CreatePayout

# 3. Check status (should be "sent" then "completed" after webhook)
grpcurl -plaintext -d '{"payout_id": "<payout_id>"}' \
  localhost:50051 orchestrator.v1.PayoutService/GetPayout
```

## Testing Fallback

To test fallback behavior, configure an invalid Stripe API key in `deploy/configs/orchestrator-local.yaml`:

```yaml
stripe:
  api_key: "sk_test_invalid"  # Invalid key triggers 401 error
```

Then create a payout - it will fail on Stripe and fall back to `mock_card`:

```bash
grpcurl -plaintext -d '{
  "idempotency_key": "failover-test",
  "request_hash": "hash",
  "amount": 5000,
  "currency": "usd",
  "rail": "card"
}' localhost:50051 orchestrator.v1.PayoutService/CreatePayout

# Response will show provider: "mock_card"
```
