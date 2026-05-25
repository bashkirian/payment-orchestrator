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
                                    │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
                                    │  │  PostgreSQL │  │    Redis    │  │      Grafana        │  │
                                    │  │  (payouts)  │  │(rate limit) │  │     :3000           │  │
                                    │  └─────────────┘  └─────────────┘  └─────────────────────┘  │
                                    │                                                             │
                                    │  ┌─────────────┐                                            │
                                    │  │ Prometheus  │◄──── scrapes /metrics from all services    │
                                    │  │   :9090     │                                            │
                                    │  └─────────────┘                                            │
                                    └─────────────────────────────────────────────────────────────┘
```

## Features

- **Multi-provider routing** - Route payouts through Stripe, mock providers, or crypto simulator
- **Automatic fallback** - On retryable errors, automatically try the next provider
- **Routing algorithms** - Priority, Weighted, and Success-based routing
- **Idempotent API** - Safe request retries with idempotency keys
- **Rate limiting** - Redis-backed token bucket rate limiter
- **Observability** - Prometheus metrics + Grafana dashboards

## Quick Start

```bash
# Start infrastructure (Postgres, Redis)
make up

# Run migrations
make migrate-up

# Build services
make build

# Run orchestrator and API
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

```bash
# Start Prometheus + Grafana
make observability-up

# Open Grafana
make grafana-open
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
