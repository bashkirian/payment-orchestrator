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
