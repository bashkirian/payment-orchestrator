# Load Testing

k6-based load testing for the payment orchestrator API.

## Prerequisites

- Docker (for running k6)
- Running API service on `localhost:8080`

## Quick Start

```bash
# Start infrastructure and services
make up
make migrate-up
make build
./bin/orchestrator start --config deploy/configs/orchestrator-local.yaml &
./bin/api &

# Run load tests
make loadtest-create
make loadtest-full-flow
make loadtest-rate-limit
```

## Available Tests

### create-payout.js

Basic payout creation load test.

- **Duration**: ~3.5 minutes
- **VUs**: Ramp up to 50, spike to 100
- **Thresholds**: p95 latency < 200ms, error rate < 1%

```bash
make loadtest-create
```

### full-flow.js

Complete payout workflow with idempotency validation.

- **Duration**: ~3 minutes
- **VUs**: 30 steady
- **Flow**: Create → Get → Idempotency retry
- **Thresholds**: p95 latency < 300ms, error rate < 1%

```bash
make loadtest-full-flow
```

### rate-limit-stress.js

Stress test for Redis token bucket rate limiter.

- **Duration**: ~50 seconds
- **VUs**: Burst to 50, wait, recovery test
- **Purpose**: Verify 429 responses under load

```bash
make loadtest-rate-limit
```

## Configuration

Override default API URL:

```bash
docker run --rm --network host -e API_URL=http://api:8080 -v $(PWD)/loadtests:/loadtests grafana/k6 run /loadtests/scripts/create-payout.js
```

## Results

Test results are saved to `loadtests/results/` as JSON files for CI integration.
