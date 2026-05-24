ROOT_APP_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
VERSION    := $(shell git describe --tags --always --dirty)
COMMIT     := $(shell git log --format="%H" -n 1)
LDFLAGS=-ldflags "-w -s -X main.version=${VERSION} -X main.commit=${COMMIT}"
TEST_FILES := `go list ./...`

# Service names
SERVICES := api orchestrator webhook worker router

COMPOSE_FILE := docker-compose.yml
COMPOSE_PROJECT := fintech-local
MIGRATIONS_DIR := db/migrations

POSTGRES_HOST ?= 127.0.0.1
POSTGRES_PORT ?= 5432
POSTGRES_ADMIN_USER ?= fintech_admin
POSTGRES_ADMIN_PASSWORD ?= fintech_admin_change_me
POSTGRES_APP_DB ?= fintech
POSTGRES_APP_USER ?= fintech_app
POSTGRES_APP_PASSWORD ?= fintech_app_change_me
REDIS_PORT ?= 6379
REDIS_PASSWORD ?= redis_change_me

GOOSE_IMAGE := mvdan/goose:latest
GOOSE_DSN := postgres://$(POSTGRES_APP_USER):$(POSTGRES_APP_PASSWORD)@$(POSTGRES_HOST):$(POSTGRES_PORT)/$(POSTGRES_APP_DB)?sslmode=disable

.PHONY: help
help: ## List of commands
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: tidy
tidy: ## Run mod tidy for all services
	@for svc in $(SERVICES); do \
		if [ -f services/$$svc/go.mod ]; then \
			echo "Tidying $$svc..."; \
			cd services/$$svc && go mod tidy -go=1.23 && cd ../..; \
		fi; \
	done

.PHONY: fmt
fmt: ## Run go fmt and goimports for all services
	@for svc in $(SERVICES); do \
		if [ -d services/$$svc ]; then \
			echo "Formatting $$svc..."; \
			cd services/$$svc && go fmt ./... && cd ../..; \
		fi; \
	done
	$(GOPATH_BIN)/goimports -w ./services

.PHONY: lint
lint: ## Run linter for all services
	golangci-lint run -v ./services/...

.PHONY: test
test: ## Run tests for all services
	@go test ${TEST_FILES}

.PHONY: cover
cover: ## Run tests with cover for all services
	@go test ${TEST_FILES} -v -coverprofile profile.cov
	@go tool cover -func profile.cov

.PHONY: cover-html
cover-html: cover ## Run tests with cover and open report
	@go tool cover -html=profile.cov

# Build targets for each service
.PHONY: build-api
build-api: ## Build API service
	CGO_ENABLED=0 go build ${LDFLAGS} -o ./bin/api ./services/api/cmd/server

.PHONY: build-orchestrator
build-orchestrator: ## Build Orchestrator service
	CGO_ENABLED=0 go build ${LDFLAGS} -o ./bin/orchestrator ./services/orchestrator/cmd/server

.PHONY: build-webhook
build-webhook: ## Build Webhook service
	CGO_ENABLED=0 go build ${LDFLAGS} -o ./bin/webhook ./services/webhook/cmd/server

.PHONY: build-worker
build-worker: ## Build Worker service
	CGO_ENABLED=0 go build ${LDFLAGS} -o ./bin/worker ./services/worker/cmd/server

.PHONY: build-router
build-router: ## Build Router service
	CGO_ENABLED=0 go build ${LDFLAGS} -o ./bin/router ./services/router/cmd/server

.PHONY: build
build: build-api build-orchestrator build-webhook ## Build all services

.PHONY: build-race
build-race: ## Build all services with race flag
	CGO_ENABLED=1 go build -race -o ./bin/api ./services/api/cmd/server
	CGO_ENABLED=1 go build -race -o ./bin/orchestrator ./services/orchestrator/cmd/server
	CGO_ENABLED=1 go build -race -o ./bin/webhook ./services/webhook/cmd/server

# Run targets
.PHONY: run-api
run-api: ## Run API service
	./bin/api

.PHONY: api-up
api-up: ## Start API and its local dependencies in Docker Compose
	docker compose -p $(COMPOSE_PROJECT) -f $(COMPOSE_FILE) up -d --wait postgres redis api

.PHONY: api-logs
api-logs: ## Show logs for API and its dependencies
	docker compose -p $(COMPOSE_PROJECT) -f $(COMPOSE_FILE) logs -f api postgres redis

.PHONY: run-orchestrator
run-orchestrator: ## Run Orchestrator service
	./bin/orchestrator start --config ./deploy/configs/orchestrator-local.yaml

.PHONY: run-webhook
run-webhook: ## Run Webhook service
	./bin/webhook start --config ./deploy/configs/webhook-local.yaml

# Proto generation
MODULE := github.com/bashkirian/fintech-project
GOPATH_BIN := $(shell go env GOPATH)/bin

.PHONY: proto-gen
proto-gen: ## Generate Go code from all proto files into libs/genproto/
	@find ./proto -name "*.proto" | while read proto; do \
		echo "Generating $$proto..."; \
		protoc \
			--plugin=protoc-gen-go="$(GOPATH_BIN)/protoc-gen-go" \
			--plugin=protoc-gen-go-grpc="$(GOPATH_BIN)/protoc-gen-go-grpc" \
			--go_out=. --go_opt=module=$(MODULE) \
			--go-grpc_out=. --go-grpc_opt=module=$(MODULE) \
			--proto_path=proto \
			$$proto; \
	done

# SQLC generation
.PHONY: sqlc-gen
sqlc-gen: ## Generate SQLC code for all services
	@for svc in $(SERVICES); do \
		if [ -f services/$$svc/contracts/pgsql/sqlc/sqlc.yaml ]; then \
			echo "Generating SQLC for $$svc..."; \
			cd services/$$svc && sqlc generate -f contracts/pgsql/sqlc/sqlc.yaml && cd ../..; \
		fi; \
	done

# Migration targets
.PHONY: create-migration
create-migration: ## Create a new migration (usage: make create-migration name=add_users_table)
	@if [ -z "$(name)" ]; then \
		echo "Error: name parameter is required. Usage: make create-migration name=add_users_table"; \
		exit 1; \
	fi
	docker run --rm \
		-v $(ROOT_APP_DIR)/$(MIGRATIONS_DIR):/migrations \
		$(GOOSE_IMAGE) -dir /migrations create $(name) sql

GOOSE := $(shell which goose 2>/dev/null || echo $(HOME)/go/bin/goose)

.PHONY: migrate-up
migrate-up: ## Run migrations up against local Postgres (requires goose: go install github.com/pressly/goose/v3/cmd/goose@latest)
	@$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "postgres://$(POSTGRES_APP_USER):$(POSTGRES_APP_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_APP_DB)?sslmode=disable" up

.PHONY: migrate-down
migrate-down: ## Roll back one migration against local Postgres
	@$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "postgres://$(POSTGRES_APP_USER):$(POSTGRES_APP_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_APP_DB)?sslmode=disable" down

.PHONY: migrate-status
migrate-status: ## Show migration status against local Postgres
	@$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "postgres://$(POSTGRES_APP_USER):$(POSTGRES_APP_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_APP_DB)?sslmode=disable" status

# Mock generation
.PHONY: mocks
mocks: ## Generate mocks for all services
	@echo "Installing mockgen..."
	@GOBIN=$(PWD)/bin go install github.com/golang/mock/mockgen@latest
	@echo "Generating mocks..."
	@for svc in $(SERVICES); do \
		if [ -d services/$$svc/internal ]; then \
			find ./services/$$svc/internal -name "*.go" -type f -not -path "*/mocks/*" -not -path "*/api/*" | xargs grep -l "interface {" 2>/dev/null | while read -r file; do \
				dir=$$(dirname "$$file"); \
				mockdir="$$dir/mocks"; \
				mkdir -p "$$mockdir"; \
				echo "Generating mocks for $$file"; \
				$(PWD)/bin/mockgen -source="$$file" -destination="$$mockdir/mock_$$(basename $$file)" -package=mocks; \
			done; \
		fi; \
	done

.PHONY: format
format: ## Install and run goimports
	@echo "Installing goimports..."
	@GOBIN=$(PWD)/bin go install golang.org/x/tools/cmd/goimports@v0.23.0
	@echo "Formatting imports..."
	bin/goimports -w ./services
	@echo "Done"

# Docker targets
.PHONY: up
up: ## Start Postgres and Redis and wait until both are healthy
	docker compose -p $(COMPOSE_PROJECT) -f $(COMPOSE_FILE) up -d --wait postgres redis
	@echo "Dependencies are healthy and reachable: Postgres on localhost:$(POSTGRES_PORT), Redis on localhost:$(REDIS_PORT)."

.PHONY: down
down: ## Stop local dependencies
	docker compose -p $(COMPOSE_PROJECT) -f $(COMPOSE_FILE) down

.PHONY: reset
reset: ## Stop dependencies and delete persistent volumes
	docker compose -p $(COMPOSE_PROJECT) -f $(COMPOSE_FILE) down -v

.PHONY: local-up
local-up: ## Start local development environment
	$(MAKE) api-up

.PHONY: local-down
local-down: ## Stop local development environment
	$(MAKE) down

.PHONY: local-logs
local-logs: ## Show logs for local environment
	docker compose -p $(COMPOSE_PROJECT) -f $(COMPOSE_FILE) logs -f

# Helm targets
.PHONY: helm-install
helm-install: ## Install helm chart (usage: make helm-install env=dev)
	helm upgrade --install fintech ./deploy/helm -n $(env)

.PHONY: helm-uninstall
helm-uninstall: ## Uninstall helm chart (usage: make helm-uninstall env=dev)
	helm uninstall fintech -n $(env)

# Clean
.PHONY: clean
clean: ## Clean build artifacts
	rm -rf ./bin
	rm -f profile.cov

# Observability
.PHONY: observability-up
observability-up: ## Start Prometheus and Grafana
	docker compose -p $(COMPOSE_PROJECT) up -d prometheus grafana
	@echo "Prometheus: http://localhost:9090"
	@echo "Grafana: http://localhost:3000 (admin/admin)"

.PHONY: observability-down
observability-down: ## Stop Prometheus and Grafana
	docker compose -p $(COMPOSE_PROJECT) stop prometheus grafana

.PHONY: grafana-open
grafana-open: ## Open Grafana in browser
	open http://localhost:3000

.PHONY: prometheus-open
prometheus-open: ## Open Prometheus in browser
	open http://localhost:9090

# Demo
.PHONY: demo
demo: ## Run local demo: start orchestrator and webhook services (requires make up && make build)
	@echo "Starting orchestrator..."
	@./bin/orchestrator start --config ./deploy/configs/orchestrator-local.yaml &
	@sleep 2
	@echo "Starting webhook..."
	@./bin/webhook start --config ./deploy/configs/webhook-local.yaml

.PHONY: demo-down
demo-down: ## Stop demo services
	@pkill -f "bin/orchestrator" 2>/dev/null || true
	@pkill -f "bin/webhook" 2>/dev/null || true
	@echo "Demo services stopped"

.PHONY: demo-stripe
demo-stripe: ## Start Stripe CLI webhook forwarder (runs in foreground)
	@echo "Starting Stripe webhook forwarder..."
	@echo "Webhooks will be forwarded to http://localhost:8082/v1/webhooks/stripe"
	@stripe listen --forward-to localhost:8082/v1/webhooks/stripe

.PHONY: demo-trigger
demo-trigger: ## Trigger test Stripe webhook events
	@echo "Triggering payment_intent.succeeded..."
	@stripe trigger payment_intent.succeeded
	@echo ""
	@echo "Triggering payment_intent.payment_failed..."
	@stripe trigger payment_intent.payment_failed
	@echo ""
	@echo "Triggering payment_intent.canceled..."
	@stripe trigger payment_intent.canceled

# Load Testing
K6_IMAGE := grafana/k6:latest
K6_URL := -e API_URL=http://host.docker.internal:8080

.PHONY: loadtest-create
loadtest-create: ## Run create payout load test
	docker run --rm --network host $(K6_URL) -v $(PWD)/loadtests:/loadtests $(K6_IMAGE) run /loadtests/scripts/create-payout.js

.PHONY: loadtest-full-flow
loadtest-full-flow: ## Run full payout flow load test
	docker run --rm --network host $(K6_URL) -v $(PWD)/loadtests:/loadtests $(K6_IMAGE) run /loadtests/scripts/full-flow.js

.PHONY: loadtest-rate-limit
loadtest-rate-limit: ## Run rate limiter stress test
	docker run --rm --network host $(K6_URL) -v $(PWD)/loadtests:/loadtests $(K6_IMAGE) run /loadtests/scripts/rate-limit-stress.js

.PHONY: loadtest-all
loadtest-all: loadtest-create loadtest-full-flow loadtest-rate-limit ## Run all load tests
