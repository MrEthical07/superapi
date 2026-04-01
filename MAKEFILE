GO ?= go
SQLC ?= sqlc
MIGRATE ?= migrate
DB_URL ?=
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

.PHONY: fmt vet test tidy run db-sync sqlc-generate migrate-create migrate-up migrate-down migrate-version module auth auth-config perf-token load-k6-10k load-vegeta-10k bench-hotpath verify

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./... -race

tidy:
	$(GO) mod tidy

run:
	$(GO) run .

db-sync:
	$(GO) run ./cmd/modulesync

sqlc-generate:
	$(MAKE) db-sync
	$(SQLC) generate

migrate-create:
	@if [ -z "$(NAME)" ]; then echo "NAME is required"; exit 1; fi
	$(MIGRATE) create -ext sql -dir db/migrations -seq $(NAME)

migrate-up:
	@if [ -z "$(DB_URL)" ]; then echo "DB_URL is required"; exit 1; fi
	POSTGRES_ENABLED=true POSTGRES_URL="$(DB_URL)" $(GO) run ./cmd/migrate up

migrate-down:
	@if [ -z "$(DB_URL)" ]; then echo "DB_URL is required"; exit 1; fi
	POSTGRES_ENABLED=true POSTGRES_URL="$(DB_URL)" $(GO) run ./cmd/migrate down --steps=1

migrate-version:
	@if [ -z "$(DB_URL)" ]; then echo "DB_URL is required"; exit 1; fi
	POSTGRES_ENABLED=true POSTGRES_URL="$(DB_URL)" $(GO) run ./cmd/migrate version

module:
	$(GO) run ./cmd/modulegen $(if $(name),--name "$(name)",) $(if $(force),--force "$(force)",) $(if $(db),--db=$(db),) $(if $(auth),--auth=$(auth),) $(if $(tenant),--tenant=$(tenant),) $(if $(ratelimit),--ratelimit=$(ratelimit),) $(if $(cache),--cache=$(cache),) $(if $(migration),--migration=$(migration),)

auth:
	$(GO) run ./cmd/authgen

auth-config:
	@if [ -z "$(file)" ]; then echo "file is required: make auth-config file=authgen.yaml"; exit 1; fi
	$(GO) run ./cmd/authgen --config "$(file)"

perf-token:
	$(GO) run ./cmd/perftoken --output json

load-k6-10k:
	@if [ -z "$(PERF_AUTH_IDENTIFIER)" ]; then echo "PERF_AUTH_IDENTIFIER is required"; exit 1; fi
	@if [ -z "$(PERF_AUTH_PASSWORD)" ]; then echo "PERF_AUTH_PASSWORD is required"; exit 1; fi
	BASE_URL="$(PERF_BASE_URL)" AUTH_IDENTIFIER="$(PERF_AUTH_IDENTIFIER)" AUTH_PASSWORD="$(PERF_AUTH_PASSWORD)" AUTH_IDENTIFIER_PREFIX="$(PERF_AUTH_IDENTIFIER_PREFIX)" AUTH_IDENTIFIER_DOMAIN="$(PERF_AUTH_IDENTIFIER_DOMAIN)" AUTH_USER_POOL_SIZE="$(PERF_AUTH_USER_POOL_SIZE)" TARGET_RPS="$(PERF_RATE)" START_RPS="$(PERF_START_RATE)" RAMP_DURATION="$(PERF_RAMP)" SUSTAIN_DURATION="$(PERF_SUSTAIN)" RAMP_STEPS="$(PERF_RAMP_STEPS)" REFRESH_BUFFER_SECONDS="$(PERF_REFRESH_BUFFER_SECONDS)" SUSTAIN_RPS_RATIO="$(PERF_SUSTAIN_RPS_RATIO)" $(K6) run --summary-export "$(PERF_OUTPUT_DIR)/k6-summary.json" performance/k6/scenario.js

load-vegeta-10k:
	@if [ -z "$(PERF_AUTH_TOKEN)" ]; then echo "PERF_AUTH_TOKEN is required"; exit 1; fi
	powershell -ExecutionPolicy Bypass -File performance/vegeta/run.ps1 -BaseUrl "$(PERF_BASE_URL)" -AuthToken "$(PERF_AUTH_TOKEN)" -Rate "$(PERF_RATE)" -RampDuration "$(PERF_RAMP)" -SustainDuration "$(PERF_SUSTAIN)" -OutputDir "$(PERF_OUTPUT_DIR)/vegeta"

bench-hotpath:
	$(GO) test ./internal/core/httpx ./internal/core/policy ./internal/core/readiness -run=^$$ -bench=. -benchmem

verify:
	$(GO) run ./cmd/superapi-verify ./...
