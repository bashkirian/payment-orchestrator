# Webhook Service Demo

This guide shows how to run the webhook service locally and test Stripe webhook integration.

## Prerequisites

- Go 1.23+
- Docker & Docker Compose
- [Stripe CLI](https://stripe.com/docs/stripe-cli) (optional, for realistic webhook testing)

## Quick Start

### 1. Start Infrastructure

```bash
# Start Postgres and Redis
make up

# Run migrations
make migrate-up
```

**Note:** Make sure no local Postgres is running on port 5432. If you have one:
```bash
# Stop local postgres (macOS)
brew services stop postgresql@15
```

### 2. Build Services

```bash
make build
```

### 3. Run Demo (Both Services)

```bash
# Terminal 1: Start both services
make demo
```

Or start them separately:

```bash
# Terminal 1: Orchestrator
./bin/orchestrator start --config ./deploy/configs/orchestrator-local.yaml

# Terminal 2: Webhook
./bin/webhook start --config ./deploy/configs/webhook-local.yaml
```

The webhook service runs on `http://localhost:8082`.

### 4. Verify Services Are Running

```bash
curl http://localhost:8082/healthz
# Expected: {"status":"ok"}

curl http://localhost:8081/healthz
# Expected: {"status":"ok"}
```

## Testing with Stripe CLI

### 1. Install Stripe CLI

```bash
# macOS
brew install stripe/stripe-cli/stripe

# Or download from https://stripe.com/docs/stripe-cli
```

### 2. Login to Stripe

```bash
stripe login
```

### 3. Forward Webhooks to Local Server

The Stripe CLI acts as a proxy between Stripe's servers and your local webhook endpoint.

```bash
# Terminal 3: Start Stripe forwarder
make demo-stripe
```

Output will look like:
```
Ready! Your webhook signing secret is whsec_xxxxxxxxxxxxx
```

**Important:** Copy the webhook signing secret (`whsec_xxx`) and update your config:

```yaml
# deploy/configs/webhook-local.yaml
stripe_webhook_secret: "whsec_xxxxxxxxxxxxx"
```

Restart the webhook service with the new secret.

### 4. Trigger Test Events

```bash
# Terminal 4: Trigger test events
make demo-trigger
```

Or trigger individual events:

```bash
stripe trigger payment_intent.succeeded
stripe trigger payment_intent.payment_failed
stripe trigger payment_intent.canceled
```

### 5. Verify in Logs

You should see logs like:

```
INFO  received stripe event  event_id=evt_xxx external_id=pi_xxx type=payout_succeeded
INFO  event processed  event_id=evt_xxx external_id=pi_xxx
```

## Event Flow

```
Stripe ──webhook──> POST /v1/webhooks/stripe
                            │
                            ▼
                    Verify signature
                            │
                            ▼
                    Parse to ProviderEvent
                            │
                            ▼
                    Redis SETNX dedup
                            │
                            ▼
                    gRPC: ApplyProviderUpdate
                            │
                            ▼
                    Orchestrator (stub)
```

## Supported Events

| Stripe Event | Normalized Type |
|-------------|-----------------|
| `payment_intent.succeeded` | `payout_succeeded` |
| `payment_intent.payment_failed` | `payout_failed` |
| `payment_intent.canceled` | `payout_canceled` |

## Deduplication

Events are deduplicated using Redis SETNX with a 24-hour TTL:

- Key format: `webhook:event:{event_id}`
- Prevents duplicate processing on webhook retries
- Safe for at-least-once delivery

## Troubleshooting

### Port 5432 already in use

A local Postgres might be running. Stop it:

```bash
# Find and stop local postgres
brew services stop postgresql@15
# Or kill the process
lsof -i :5432
kill <PID>
```

### Invalid signature

Ensure `stripe_webhook_secret` in config matches the secret from `stripe listen`.

### Connection refused to Redis

```bash
# Check Redis is running
docker ps | grep redis

# Or restart infrastructure
make down && make up
```

### Orchestrator not reachable

```bash
# Check orchestrator is running
lsof -i :50051

# Start it
./bin/orchestrator start --config ./deploy/configs/orchestrator-local.yaml
```

### View Redis keys

```bash
docker exec -it fintech-redis redis-cli -a change_me_redis_password
> KEYS webhook:*
> TTL webhook:event:evt_xxx
```

### Stop Demo Services

```bash
make demo-down
```

## Make Commands Summary

| Command | Description |
|---------|-------------|
| `make up` | Start Postgres + Redis |
| `make down` | Stop all containers |
| `make migrate-up` | Run database migrations |
| `make build` | Build all services |
| `make demo` | Start orchestrator + webhook |
| `make demo-down` | Stop demo services |
| `make demo-stripe` | Start Stripe CLI forwarder |
| `make demo-trigger` | Trigger test Stripe events |
