# Getting Started

From "Use this template" to a running, authenticated API.

## Prerequisites

- Go (the version in `go.mod`)
- Docker with Compose v2 (for the local Postgres + Redis)
- Optional: `sqlc` v1.31.1 if you will change SQL
  (`go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1`)

`make doctor` checks all of these.

<!-- template:begin init -->
## Create and initialize your project

```bash
# on GitHub: "Use this template" -> create acme/foo, then
git clone https://github.com/acme/foo && cd foo
make init module=github.com/acme/foo name="Foo API"
```

`make init` (`cmd/templateinit`) runs once and:

- rewrites the Go module path everywhere and runs `go mod tidy`;
- resets README title, CHANGELOG, LICENSE holder and SECURITY contact
  (add `flags="--copyright 'Acme Inc'"` to set the holder);
- removes template-maintainer content, then deletes itself.

Preview first with `make init module=... flags=--dry-run`. Optional pruning:

| Flag | Removes |
|---|---|
| `--no-tenancy` | tenant resolution, tenants table, `TENANCY_*` config |
| `--no-webauthn` | WebAuthn credential store, ceremonies, `WEBAUTHN_*` |
| `--no-document-store` | the optional NoSQL store package |
| `--no-devx` | `make module` scaffolder and module SQL sync |
| `--no-perf` | `performance/`, `cmd/perftoken`, `make load-*` |
| `--no-demo` | bundled example code |
| `--no-all` | all of the above |

Example: `make init module=github.com/acme/foo name="Foo API" flags="--no-perf --no-webauthn"`.
Every combination leaves a project that passes its own gate (CI checks the
default and `--no-all`).
<!-- template:end init -->

## Start dependencies and configure

```bash
make dev-up                 # Postgres 18 + Redis 8 (docker-compose.yml)
cp .env.example .env        # credentials already match docker-compose.yml
make migrate-up             # uses POSTGRES_URL from .env
```

## Create the first user

```bash
make user email=admin@example.com role=admin
# Password: (prompted, not echoed)
```

See [auth-bootstrap.md](auth-bootstrap.md) for piping the password, tenants
and roles.

## Run

```bash
make run
```

```bash
curl -s localhost:8080/healthz
TOKEN=$(curl -s -XPOST localhost:8080/api/v1/auth/login \
  -d '{"identifier":"admin@example.com","password":"..."}' | jq -r .data.access_token)
curl -s -H "Authorization: Bearer $TOKEN" localhost:8080/api/v1/auth/whoami
```

## Turn on what you need

All optional auth features are off by default. Enable them in `.env`:

```bash
AUTH_REGISTRATION_ENABLED=true
AUTH_PASSWORD_RESET_ENABLED=true
AUTH_EMAIL_VERIFICATION_ENABLED=true
AUTH_TOTP_ENABLED=true
AUTH_TOTP_ENCRYPTION_KEY=$(openssl rand -base64 32)   # paste the value
NOTIFY_DRIVER=log            # dev: see reset/verification messages in the log
NOTIFY_LOG_SECRETS=true      # dev only: log the full secret
```

Reset and verification messages need a real `notify.Notifier` in production;
see [auth-flows.md](auth-flows.md). Multi-tenancy: [multi-tenancy.md](multi-tenancy.md).

## Build your first module

<!-- template:begin devx -->
```bash
make module name=projects db=1
```

<!-- template:end devx -->
Then follow [modules.md](modules.md) and [crud-examples.md](crud-examples.md).
Before every commit:

```bash
go build ./... && go test ./... && go run ./cmd/superapi-verify ./...
make test-integration      # with make dev-up running: Postgres integration tests
```

## Redis licence

`docker-compose.yml` uses `redis:8`. Since Redis 8 the open-source edition is
licensed **RSALv2 / SSPLv1 / AGPLv3**, not BSD. That is fine for local
development, but templates often get copied into production, so review the
licence before running Redis 8 in your deployment. The compose file carries a
commented-out **Valkey** option (`valkey/valkey:9-alpine`), a drop-in,
BSD-3-Clause-licensed Redis fork: switch the image, command and healthcheck
lines and nothing else changes (`REDIS_ADDR` and the app code stay the same).
Managed offerings (ElastiCache/Memorystore for Valkey or Redis) are another
option.

## Ship it

```bash
docker build -t foo-api .
docker run --rm --env-file .env foo-api /app/migrate up
docker run --rm -p 8080:8080 --env-file .env foo-api
```

Before production, read [security-env-recommendations.md](security-env-recommendations.md)
and set `APP_ENV=prod` (enables stricter lint: fail-closed cache/rate limit,
metrics token, no `AUTH_TEST_*`, no `NOTIFY_LOG_SECRETS`).
