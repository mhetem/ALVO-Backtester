COMPOSE      := docker compose
COMPOSE_DEV  := docker compose -f docker-compose.yml -f docker-compose.dev.yml
COMPOSE_PROD := docker compose -f docker-compose.yml -f docker-compose.prod.yml
SQLC_IMAGE   := sqlc/sqlc:latest

.DEFAULT_GOAL := help
.PHONY: help up up-dev up-prod down restart logs ps psql migrate sqlc sync-symbols backfill sync-candles gaps candles build test vet lint sec fmt check check-frontend frontend I-KNOW-THIS-DELETES-THE-CANDLE-STORE

help:
	@grep -E '^[a-zA-Z][a-zA-Z0-9_-]*:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "} {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

up: ## Build and start app + db
	$(COMPOSE) up -d --build

up-dev: ## Start with source bind-mounts, exposed db port and the Vite dev server
	$(COMPOSE_DEV) up -d --build

up-prod: ## Start behind Caddy with TLS and resource limits
	$(COMPOSE_PROD) up -d --build

down: ## Stop everything, keep the database volume
	$(COMPOSE) down

restart: ## Restart the app container to pick up source changes in dev
	$(COMPOSE_DEV) restart app

logs: ## Follow app logs
	$(COMPOSE) logs -f app

ps: ## Show service status
	$(COMPOSE) ps

psql: ## Open a psql shell on the database
	$(COMPOSE) exec db sh -c 'psql -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"'

migrate: ## Apply pending migrations by recreating the app (goose runs on startup)
	$(COMPOSE) up -d --build --force-recreate app

sqlc: ## Regenerate internal/db from sql/schema + sql/queries
	docker run --rm -v "$(CURDIR):/src" -w /src $(SQLC_IMAGE) generate

RUN := $(COMPOSE) run --rm --build app

sync-symbols: ## Seed symbols from data/indexes + data/contracts.json. ARGS="--dry-run" to preview
	$(RUN) sync-symbols $(ARGS)

backfill: ## Fetch history into candles. ARGS="--symbol PETR4 --timeframe 1d --dry-run"
	$(RUN) backfill $(ARGS)

sync-candles: ## Refresh the last few sessions for every tracked symbol. ARGS="--timeframe 1d"
	$(RUN) sync-candles $(ARGS)

gaps: ## Report missing sessions against the trading calendar. ARGS="--symbol PETR4 --timeframe 1d"
	$(RUN) gaps $(ARGS)

candles: ## Print candles at any timeframe, resampling 15m/30m/1h from 5m on read. ARGS="--symbol PETR4 --timeframe 1h"
	$(RUN) candles $(ARGS)

frontend: ## Build the Svelte app into frontend/dist
	cd frontend && npm ci && npm run build

build: frontend ## Build the Go binary against a freshly built frontend
	go build -o ./tmp/alvo .

test: ## Run the Go test suite
	go test ./...

cover: ## Run the Go test suite with cover flag
	go test ./... -cover

vet: ## Run go vet
	go vet ./...

lint: ## Run staticcheck
	staticcheck ./...

sec: ## Run gosec
	gosec ./...

fmt: ## Format Go sources
	gofmt -w .

check-frontend: ## Type-check the Svelte app inside the web container, where its deps live
	$(COMPOSE_DEV) run --rm --no-deps web sh -c "npm install && npm run check"

check: vet lint sec test check-frontend ## Vet, lint, scan, test, then type-check the frontend
	@echo "all checks passed"

I-KNOW-THIS-DELETES-THE-CANDLE-STORE: ## Destroy the database volume. Rebuilding it costs a month of brapi Pro
	@printf 'This deletes the pgdata volume permanently. Type DELETE to confirm: ' && read ans && [ "$$ans" = DELETE ]
	$(COMPOSE) down -v
