# Dropbox-style sync POC: local development helpers.
# Run `make` (or `make help`) to see everything available.
#
# Quick start:   make setup   (once)   then   make dev

# Everything is resolved relative to this Makefile's folder, so it also works
# from another directory: `make -C path/to/project dev`.
ROOT    := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
COMPOSE ?= docker compose -f "$(ROOT)docker-compose.yml"

.DEFAULT_GOAL := help
.PHONY: help check-tools setup deps infra-up infra-down infra-reset infra-logs \
        wait-mongo server web dev status health mongo-shell

help: ## Show this help
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*## "} {printf "  %-12s %s\n", $$1, $$2}'

check-tools:
	@for t in docker go node npm; do \
		command -v $$t >/dev/null || { echo "missing required tool: $$t"; exit 1; }; \
	done
	@for d in backend frontend; do \
		test -d "$(ROOT)$$d" || { echo "folder not found next to the Makefile: $$d/"; exit 1; }; \
	done
	@test -f "$(ROOT)docker-compose.yml" || { \
		echo "docker-compose.yml not found in $(ROOT)"; \
		echo "Put docker-compose.yml in the same folder as this Makefile."; exit 1; }

# ---------------------------------------------------------------- one-time setup

setup: infra-up deps ## First-time setup: start MongoDB + MinIO, install dependencies

deps: check-tools ## Install Go and npm dependencies
	cd	"$(ROOT)backend" && go mod tidy
	cd	"$(ROOT)frontend" && npm install

# ---------------------------------------------------------------- MongoDB + MinIO (docker)

infra-up: check-tools ## Start MongoDB and MinIO (the Go server creates the bucket)
	$(COMPOSE) up -d mongo minio
	@$(MAKE) --no-print-directory wait-mongo
	@echo ""
	@echo "MongoDB       localhost:27017"
	@echo "MinIO (S3)    http://localhost:9000"
	@echo "MinIO console http://localhost:9001   (login: minioadmin / minioadmin)"

wait-mongo:
	@echo "Waiting for MongoDB to accept connections..."
	@for i in $$(seq 1 30); do \
		$(COMPOSE) exec -T mongo mongosh --quiet --eval 'db.runCommand({ping:1}).ok' >/dev/null 2>&1 && exit 0; \
		sleep 1; \
	done; echo "MongoDB did not become ready in 30s"; exit 1

infra-down: ## Stop MongoDB and MinIO (data is kept)
	$(COMPOSE) down

infra-reset: ## Stop them AND delete all stored data (files and metadata)
	$(COMPOSE) down -v

infra-logs: ## Follow MongoDB and MinIO logs
	$(COMPOSE) logs -f mongo minio

status: ## Show the state of the docker services
	$(COMPOSE) ps

# ---------------------------------------------------------------- the app

server: ## Run the Go API on http://localhost:8080
	cd	"$(ROOT)backend" && go run .

web: ## Run the React dev server on http://localhost:5173
	cd	"$(ROOT)frontend" && npm run dev

dev: infra-up ## Start everything: MongoDB + MinIO, then API and UI together (Ctrl-C stops both)
	@$(MAKE) --no-print-directory -j2 server web

health: ## Check that the API is up
	@curl -sf http://localhost:8080/api/health && echo || echo "API is not reachable on :8080"

mongo-shell: ## Open a mongo shell on the project database
	$(COMPOSE) exec mongo mongosh dropbox_poc