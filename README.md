# fintech-project
Payment orchestrator

## Start

Development is handled in docker containers

1. Copy .env.example contents into your local .env file
2. Run migrations
3. make local-up

## API environment

The API reads `API_*` variables from the process environment. For local development it also loads the root `.env` file automatically via `godotenv`.

Example `.env` values:

```env
API_ENV=development
API_HTTP_ADDR=:8080
API_LOG_LEVEL=info
API_READ_TIMEOUT=5s
API_READ_HEADER_TIMEOUT=2s
API_WRITE_TIMEOUT=10s
API_IDLE_TIMEOUT=30s
API_SHUTDOWN_TIMEOUT=10s
```

Common local flows:

```bash
make build-api && make run-api
make api-up
```

- `make run-api` runs the API binary locally and loads `.env` via the app.
- `make api-up` starts the API in Docker Compose with the same `API_*` values coming from `.env`.

## Observability

The project includes a full observability stack with Prometheus and Grafana.

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         SERVICES                                 │
│                                                                  │
│   API (:8080)      Orchestrator (:8081/50051)    Webhook (:8082)│
│   └─ /metrics      └─ /metrics                    └─ /metrics   │
└──────────────────────────────┬──────────────────────────────────┘
                               │ scrape /metrics
                               ▼
                    ┌─────────────────────┐
                    │    Prometheus       │
                    │      :9090          │
                    └──────────┬──────────┘
                               │
                               ▼
                    ┌─────────────────────┐
                    │      Grafana        │
                    │       :3000         │
                    └─────────────────────┘
```

### Quick Start

```bash
# Start infrastructure (Postgres, Redis)
make up

# Start observability stack
make observability-up

# Build and run services
make build
./bin/orchestrator start --config deploy/configs/orchestrator-local.yaml &
./bin/webhook start --config deploy/configs/webhook-local.yaml &
API_REDIS_PASSWORD="change_me_redis_password" ./bin/api &

# Open Grafana
make grafana-open
# or visit http://localhost:3000 (admin/admin)
```

### Available Metrics

| Service | Metrics |
|---------|---------|
| **API** | HTTP latency (p50/p95/p99), request counts, in-flight requests, rate limiter stats |
| **Orchestrator** | gRPC latency per method, response codes, handling time histograms |
| **Webhook** | HTTP latency, request counts, event processing stats |

### Grafana Dashboards

Pre-provisioned dashboards in the **Fintech** folder:

- **API Overview** - HTTP latency, request rate by endpoint, error rate, rate limiter stats
- **Orchestrator Overview** - gRPC latency, response code distribution, success rate
- **Webhook Overview** - Event processing latency, request counts

### Makefile Targets

```bash
make observability-up    # Start Prometheus + Grafana
make observability-down  # Stop observability stack
make grafana-open        # Open Grafana in browser
make prometheus-open     # Open Prometheus in browser
```