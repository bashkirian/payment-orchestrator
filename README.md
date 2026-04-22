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