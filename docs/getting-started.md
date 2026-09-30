# Getting Started

From "Use this template" to a running, authenticated API.

## Prerequisites

- Go 1.27 or newer (the version in `go.mod`; `make doctor` checks it)
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
- removes template-maintainer content and the template's own CI (see
  [What happens to CI](#what-happens-to-ci)), then deletes itself.

Preview first with `make init module=... flags=--dry-run`. Optional pruning:

| Flag | Removes |
|---|---|
| `--no-tenancy` | multi-tenancy: `internal/tenancy`, its migration (`000002`), sqlc schema and queries, `TENANCY_*` config and docs |
| `--no-webauthn` | WebAuthn credential store, ceremonies, `WEBAUTHN_*` |
| `--no-document-store` | the optional NoSQL store package |
| `--no-smtp` | the built-in SMTP notifier and `SMTP_*` config |
| `--no-devx` | `make module` scaffolder and module SQL sync |
| `--no-rotate-tool` | `cmd/rotatetotpkey` and `make rotate-totp-key` |
| `--no-perf` | `performance/`, `cmd/perftoken`, `make load-*` |
| `--no-demo` | bundled example code |
| `--no-all` | all of the above |

Example: `make init module=github.com/acme/foo name="Foo API" flags="--no-perf --no-webauthn"`.
Every combination leaves a project that passes its own gate (the template's CI
checks the default, `--no-tenancy` and `--no-all`). `--no-webauthn` strips the
marked WebAuthn blocks from `000001_init` instead of deleting a numbered
migration.

### What happens to CI

The template's CI has two halves, told apart by file name:

| Kept: the CI your project needs | Removed by `make init`: the CI that only tests the template |
|---|---|
| `.github/workflows/ci.yml`: build, vet, gofmt, golangci-lint, tests with Postgres and Redis (`-race`), sqlc drift, migrations up/down/up, `superapi-verify`, `govulncheck`, the Docker image and compose smoke test, and a CycloneDX SBOM | every `.github/workflows/template-*` file: `template-init.yml` (runs `make init` against a copy of the template for several flag combinations and runs each result's gate) and `template-tenancy.yml` (proves the tenancy-removal check is neither blind nor noisy) |
| the workflow of each feature you keep (for example the one that runs the suite with that feature switched on); `--no-<feature>` deletes that feature's workflow together with the feature | every `.github/template-*` file, such as the allowlist the template's removal check reads |

Why: `make init` has already run in your repository, so the jobs that exercise
it could only ever test the template, not your code, and a project that keeps
them pays for runs that prove nothing about it. Anything else you add to
`.github/workflows` is yours and untouched; name it anything but `template-*`.

The kept workflows run on every pull request, on pushes to `main` (so `main` is
tested after each merge), and on demand (`workflow_dispatch`). Pushes to other
branches do not start a run, and a new push to a pull request cancels the run it
made stale. Protect `main` by requiring the `ci` checks before merge.
<!-- template:end init -->

## Start dependencies and configure

```bash
make dev-up                 # Postgres 18 + Redis 8 (docker-compose.yml)
cp .env.example .env        # credentials already match docker-compose.yml
make migrate-up             # uses POSTGRES_URL from .env
```

### Your migrations

The template ships two migrations in `db/migrations`: `000001_init` (the auth
schema: users, TOTP, backup codes, WebAuthn) and, if you kept it, a second one
for an optional feature (`000002_<feature>`). **They are yours to edit freely
until your first deployment**: reshape `users`, add columns, delete the second
migration if you do not need it, renumber as you like, and keep the sqlc schema
mirror in `db/schema` in step. **After your first deployment migrations are
append-only**: never edit or renumber a file that has been applied anywhere;
add a new one with `make migrate-create NAME=add_projects_table`, which numbers
it after the last. See [workflows.md](workflows.md#51-migration-flow).

## Create the first user

```bash
make user email=admin@example.com role=admin
# Password: (prompted, not echoed)
```

See [auth-bootstrap.md](auth-bootstrap.md) for piping the password, extra
flags (`make user email=... flags="..."`) and roles.

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
see [auth-flows.md](auth-flows.md). Optional features:
[architecture.md](architecture.md#13-optional-features).

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

## Continuous integration

`.github/workflows/ci.yml` is your project's gate: build, vet, gofmt, lint, tests
with Postgres and Redis, sqlc drift, migrations up/down/up, `superapi-verify`,
`govulncheck`, the Docker image and an SBOM. It runs on pull requests, on pushes
to `main` and on demand; pushes to other branches do not start it, and a new
push to a pull request cancels the stale run. Require its checks before merging
into `main`. Add your own workflows next to it.

## Ship it

```bash
docker build -t foo-api .
docker run --rm --env-file .env foo-api /app/migrate up
docker run --rm -p 8080:8080 --env-file .env foo-api
```

Before production, read [security-env-recommendations.md](security-env-recommendations.md)
and set `APP_ENV=prod` (enables stricter lint: fail-closed cache/rate limit,
metrics token, no `AUTH_TEST_*`, no `NOTIFY_LOG_SECRETS`).
