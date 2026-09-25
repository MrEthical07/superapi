GO ?= go
SQLC ?= sqlc
MIGRATE ?= migrate
DOCKER_COMPOSE ?= docker compose
DB_URL ?=

# Recipes that talk to Postgres/Redis source .env (if present) so a fresh
# clone works after `cp .env.example .env`. Values in .env override the shell.
WITH_ENV = set -a; if [ -f .env ]; then . ./.env; fi; set +a;

# template:begin perf
K6 ?= k6
PERF_BASE_URL ?= http://127.0.0.1:8080
PERF_AUTH_IDENTIFIER ?= loadtest@example.com
PERF_AUTH_PASSWORD ?= LoadTest123!
PERF_AUTH_IDENTIFIER_PREFIX ?= loadtest
PERF_AUTH_IDENTIFIER_DOMAIN ?= example.com
PERF_AUTH_USER_POOL_SIZE ?= 500
PERF_RATE ?= 10000
PERF_START_RATE ?= 500
PERF_RAMP ?= 10m
PERF_SUSTAIN ?= 30m
PERF_RAMP_STEPS ?= 10
PERF_REFRESH_BUFFER_SECONDS ?= 30
PERF_SUSTAIN_RPS_RATIO ?= 0.95
PERF_OUTPUT_DIR ?= performance/results
# template:end perf

.PHONY: fmt vet test test-integration tidy build run verify doctor dev-up dev-down dev-reset db-sync sqlc-generate migrate-create migrate-up migrate-down migrate-version module user perf-token load-k6-10k load-vegeta-10k bench-hotpath init

# ---------------------------------------------------------------------------
# Quality gates
# ---------------------------------------------------------------------------

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./... -race

# Runs the suite with Postgres integration tests enabled (needs `make dev-up`).
test-integration:
	@$(WITH_ENV) url="$(DB_URL)"; [ -n "$$url" ] || url="$$POSTGRES_URL"; \
	if [ -z "$$url" ]; then echo "set DB_URL or POSTGRES_URL (in .env)"; exit 1; fi; \
	SUPERAPI_TEST_DATABASE_URL="$$url" $(GO) test ./... -race

verify:
	$(GO) run ./cmd/superapi-verify ./...

tidy:
	$(GO) mod tidy

build:
	$(GO) build ./...

# ---------------------------------------------------------------------------
# Run
# ---------------------------------------------------------------------------

run:
	@$(WITH_ENV) $(GO) run ./cmd/api

# Create an account through the configured goAuth engine. The password is
# prompted for without echo (or piped with password_stdin=1); never pass it as
# a variable. Example: make user email=admin@example.com role=admin
user:
	@if [ -z "$(email)" ]; then echo "email is required: make user email=you@example.com [role=admin] [tenant=acme] [create_tenant=1]"; exit 1; fi
	@$(WITH_ENV) $(GO) run ./cmd/createuser --email "$(email)" $(if $(role),--role "$(role)",) $(if $(tenant),--tenant "$(tenant)",) $(if $(create_tenant),--create-tenant,) $(if $(password_stdin),--password-stdin,)

# Checks the local toolchain and configuration.
doctor:
	@ok=1; \
	printf '%-10s' "go:";      if command -v $(GO) >/dev/null 2>&1; then $(GO) version; else echo "MISSING (https://go.dev/dl/)"; ok=0; fi; \
	want=$$(sed -n 's/^go //p' go.mod); printf '%-10s%s\n' "go.mod:" "requires go $$want"; \
	printf '%-10s' "sqlc:";    if command -v $(SQLC) >/dev/null 2>&1; then $(SQLC) version; else echo "missing (needed for make sqlc-generate: go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1)"; fi; \
	printf '%-10s' "docker:";  if $(DOCKER_COMPOSE) version >/dev/null 2>&1; then $(DOCKER_COMPOSE) version --short; else echo "missing (needed for make dev-up)"; fi; \
	printf '%-10s' "migrate:"; echo "built in (go run ./cmd/migrate)"; \
	printf '%-10s' ".env:";    if [ -f .env ]; then echo "present"; else echo "missing (cp .env.example .env)"; fi; \
	[ $$ok = 1 ]

# ---------------------------------------------------------------------------
# Local dependencies (docker-compose.yml: Postgres 18 + Redis 8)
# ---------------------------------------------------------------------------

dev-up:
	$(DOCKER_COMPOSE) up -d --wait

dev-down:
	$(DOCKER_COMPOSE) down

# Destroys local Postgres/Redis data and starts fresh.
dev-reset:
	$(DOCKER_COMPOSE) down -v
	$(DOCKER_COMPOSE) up -d --wait

# ---------------------------------------------------------------------------
# Database (DB_URL defaults to POSTGRES_URL from the environment or .env)
# ---------------------------------------------------------------------------

sqlc-generate:
# template:begin devx
	$(MAKE) db-sync
# template:end devx
	$(SQLC) generate

migrate-create:
	@if [ -z "$(NAME)" ]; then echo "NAME is required"; exit 1; fi
	$(MIGRATE) create -ext sql -dir db/migrations -seq $(NAME)

migrate-up migrate-down migrate-version: migrate-%:
	@$(WITH_ENV) url="$(DB_URL)"; [ -n "$$url" ] || url="$$POSTGRES_URL"; \
	if [ -z "$$url" ]; then echo "DB_URL is required (or set POSTGRES_URL in .env)"; exit 1; fi; \
	case "$*" in down) args="down --steps=1";; *) args="$*";; esac; \
	POSTGRES_ENABLED=true POSTGRES_URL="$$url" $(GO) run ./cmd/migrate $$args

# template:begin devx
# ---------------------------------------------------------------------------
# Scaffolding
# ---------------------------------------------------------------------------

db-sync:
	$(GO) run ./cmd/modulesync

module:
	$(GO) run ./cmd/modulegen $(if $(name),--name "$(name)",) $(if $(force),--force "$(force)",) $(if $(db),--db=$(db),) $(if $(auth),--auth=$(auth),) $(if $(tenant),--tenant=$(tenant),) $(if $(ratelimit),--ratelimit=$(ratelimit),) $(if $(cache),--cache=$(cache),) $(if $(migration),--migration=$(migration),)
# template:end devx

# template:begin init
# ---------------------------------------------------------------------------
# One-time project initialization (removes itself afterwards)
# make init module=github.com/acme/foo name="Foo API" [flags="--no-tenancy --dry-run"]
# ---------------------------------------------------------------------------

init:
	@if [ -z "$(module)" ]; then echo 'module is required: make init module=github.com/acme/foo name="Foo API"'; exit 1; fi
	$(GO) run ./cmd/templateinit --module "$(module)" $(if $(name),--name "$(name)",) $(flags)
# template:end init

bench-hotpath:
	$(GO) test ./internal/core/httpx ./internal/core/policy ./internal/core/readiness -run=^$$ -bench=. -benchmem

# template:begin perf
# ---------------------------------------------------------------------------
# Load testing (performance/; PowerShell scripts with POSIX ports for vegeta and seeding)
# ---------------------------------------------------------------------------

perf-token:
	@$(WITH_ENV) $(GO) run ./cmd/perftoken --create-if-missing --output json

load-k6-10k:
	@if [ -z "$(PERF_AUTH_IDENTIFIER)" ]; then echo "PERF_AUTH_IDENTIFIER is required"; exit 1; fi
	@if [ -z "$(PERF_AUTH_PASSWORD)" ]; then echo "PERF_AUTH_PASSWORD is required"; exit 1; fi
	BASE_URL="$(PERF_BASE_URL)" AUTH_IDENTIFIER="$(PERF_AUTH_IDENTIFIER)" AUTH_PASSWORD="$(PERF_AUTH_PASSWORD)" AUTH_IDENTIFIER_PREFIX="$(PERF_AUTH_IDENTIFIER_PREFIX)" AUTH_IDENTIFIER_DOMAIN="$(PERF_AUTH_IDENTIFIER_DOMAIN)" AUTH_USER_POOL_SIZE="$(PERF_AUTH_USER_POOL_SIZE)" TARGET_RPS="$(PERF_RATE)" START_RPS="$(PERF_START_RATE)" RAMP_DURATION="$(PERF_RAMP)" SUSTAIN_DURATION="$(PERF_SUSTAIN)" RAMP_STEPS="$(PERF_RAMP_STEPS)" REFRESH_BUFFER_SECONDS="$(PERF_REFRESH_BUFFER_SECONDS)" SUSTAIN_RPS_RATIO="$(PERF_SUSTAIN_RPS_RATIO)" $(K6) run --summary-export "$(PERF_OUTPUT_DIR)/k6-summary.json" performance/k6/scenario.js

load-vegeta-10k:
	@if [ -z "$(PERF_AUTH_TOKEN)" ]; then echo "PERF_AUTH_TOKEN is required"; exit 1; fi
	@if command -v powershell >/dev/null 2>&1; then \
		powershell -ExecutionPolicy Bypass -File performance/vegeta/run.ps1 -BaseUrl "$(PERF_BASE_URL)" -AuthToken "$(PERF_AUTH_TOKEN)" -Rate "$(PERF_RATE)" -RampDuration "$(PERF_RAMP)" -SustainDuration "$(PERF_SUSTAIN)" -OutputDir "$(PERF_OUTPUT_DIR)/vegeta"; \
	else \
		performance/vegeta/run.sh -b "$(PERF_BASE_URL)" -t "$(PERF_AUTH_TOKEN)" -r "$(PERF_RATE)" -u "$(PERF_RAMP)" -s "$(PERF_SUSTAIN)" -o "$(PERF_OUTPUT_DIR)/vegeta"; \
	fi
# template:end perf
