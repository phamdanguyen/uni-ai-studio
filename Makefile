.PHONY: dev build test lint clean infra infra-down migrate

# --- Development ---

dev: ## Run the server in development mode
	go run ./cmd/server

build: ## Build the server binary
	go build -o bin/waoo-server ./cmd/server
	go build -o bin/waoo-worker ./cmd/worker
	go build -o bin/waoo-cli ./cmd/cli

test: ## Run all tests
	go test -race -cover ./...

lint: ## Run linter
	golangci-lint run ./...

clean: ## Clean build artifacts
	rm -rf bin/

# --- Infrastructure ---

infra: ## Start infrastructure (PostgreSQL, NATS, Redis, MinIO)
	docker compose up -d

infra-down: ## Stop infrastructure
	docker compose down

infra-reset: ## Reset infrastructure (destroy volumes)
	docker compose down -v
	docker compose up -d

# --- Database ---

migrate: ## Apply database migrations
	@echo "Applying migrations..."
	@for f in migrations/*.sql; do \
		echo "  $$f"; \
		PGPASSWORD=waoo_secret psql -h localhost -U waoo -d waoo_studio -f "$$f"; \
	done

# --- Go tools ---

deps: ## Install dependencies
	go mod tidy

generate: ## Run code generation
	go generate ./...

# --- Help ---

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
