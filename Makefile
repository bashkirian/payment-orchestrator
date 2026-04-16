ROOT_APP_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
VERSION    := $(shell git describe --tags --always --dirty)
COMMIT     := $(shell git log --format="%H" -n 1)
LDFLAGS=-ldflags "-w -s -X main.version=${VERSION} -X main.commit=${COMMIT}"
TEST_FILES := `go list ./...`

# Service names
SERVICES := api orchestrator webhook worker router

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
	goimports -w ./services

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
	./bin/api start --config ./deploy/configs/api-local.yaml

.PHONY: run-orchestrator
run-orchestrator: ## Run Orchestrator service
	./bin/orchestrator start --config ./deploy/configs/orchestrator-local.yaml

.PHONY: run-webhook
run-webhook: ## Run Webhook service
	./bin/webhook start --config ./deploy/configs/webhook-local.yaml

# Proto generation
.PHONY: proto-gen
proto-gen: ## Generate code from proto files
	@for proto in $(shell find ./proto -name "*.proto"); do \
		echo "Generating $$proto..."; \
		protoc --go_out=. --go-grpc_out=. $$proto; \
	done

# SQLC generation
.PHONY: sqlc-gen
sqlc-gen: ## Generate SQLC code for all services
	@for svc in $(SERVICES); do \
		if [ -f services/$$svc/contracts/pgsql/sqlc/sqlc.yaml ]; then \
			echo "Generating SQLC for $$svc..."; \
			cd services/$$svc && sqlc generate && cd ../..; \
		fi; \
	done

# Migration targets
.PHONY: create-migration
create-migration: ## Create a new migration (usage: make create-migration name=migration_name svc=api)
	@if [ -z "$(svc)" ]; then \
		echo "Error: svc parameter is required. Usage: make create-migration name=my_migration svc=api"; \
		exit 1; \
	fi
	goose -dir services/$(svc)/contracts/pgsql/migrations create $(name) sql

.PHONY: migrate-up
migrate-up: ## Run migrations up (usage: make migrate-up svc=api)
	@if [ -z "$(svc)" ]; then \
		echo "Error: svc parameter is required. Usage: make migrate-up svc=api"; \
		exit 1; \
	fi
	goose -dir services/$(svc)/contracts/pgsql/migrations up

.PHONY: migrate-down
migrate-down: ## Run migrations down (usage: make migrate-down svc=api)
	@if [ -z "$(svc)" ]; then \
		echo "Error: svc parameter is required. Usage: make migrate-down svc=api"; \
		exit 1; \
	fi
	goose -dir services/$(svc)/contracts/pgsql/migrations down

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
.PHONY: local-up
local-up: ## Start local development environment
	docker-compose -f deploy/docker-compose-local.yml up -d

.PHONY: local-down
local-down: ## Stop local development environment
	docker-compose -f deploy/docker-compose-local.yml down

.PHONY: local-logs
local-logs: ## Show logs for local environment
	docker-compose -f deploy/docker-compose-local.yml logs -f

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
