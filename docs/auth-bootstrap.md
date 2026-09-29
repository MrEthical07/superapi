# Auth Bootstrap: Users, Schema and Roles

How accounts get created, what the users schema looks like, and how roles and
permissions work.

## 1. Create accounts with `make user`

Public registration is off by default, so the first account is created from
the command line through the same goAuth engine the server uses
(`cmd/createuser`, built with `app.NewDependencies`):

```bash
make user email=admin@example.com role=admin
# Password:           (not echoed)
# Confirm password:
```

- The password is **never** a flag or Make variable (that would land in shell
  history and process listings). It is prompted without echo, or piped:
  `printf '%s\n' "$PW" | make user email=ci@example.com password_stdin=1`.
- `role` must exist in `internal/core/auth/roles.go` (default `user`).
- With `AUTH_EMAIL_VERIFICATION_ENABLED=true` the account is activated
  immediately (the operator vouches for the address); pass
  `--require-verification` to `go run ./cmd/createuser` to leave it pending.
- Extra flags are passed straight through: `make user email=... flags="--require-verification"`.
  Optional features can add their own flags to `createuser` (they show up in
  `go run ./cmd/createuser --help` when the feature is present).
- goAuth's password policy applies; a duplicate email is reported as such.

Direct invocation: `go run ./cmd/createuser --help`. Container image:
`docker run --rm -it --env-file .env <image> /app/createuser --email … --role admin`.

## 2. The users schema

The template ships two migrations in `db/migrations/`:

- `000001_init` is the baseline: `users`, the TOTP and backup-code tables, and
  the WebAuthn credentials table.
- `000002_*` is optional. It belongs to an optional feature (see
  [trim-to-what-you-need.md](trim-to-what-you-need.md)), adds that feature's
  tables and columns, and is deleted together with the feature when it is
  pruned.

Both are yours to edit freely until your project's first deployment (reshape
`users`, add columns, drop what you do not need). After the first deployment
migrations are append-only: add a new numbered file with
`make migrate-create NAME=...`.

The baseline is mirrored for sqlc in `db/schema/auth_users.sql`,
`db/schema/auth_mfa.sql` and `db/schema/webauthn_credentials.sql`. The `users`
table:

| Column | Purpose |
|---|---|
| `id UUID` | user id (goAuth `UserID`) |
| `email TEXT` | login identifier, always lower-case: `CHECK (email = lower(email))` plus a unique index on `lower(email)` (one account per address) |
| `password_hash TEXT` | Argon2id hash produced by goAuth |
| `role TEXT` | role name from `roles.go` |
| `permissions BIGINT` | reserved (permissions come from the role registry) |
| `status TEXT` | `active`, `pending_verification`, `disabled`, `locked`, `deleted` |
| `account_version INT` | advanced on status/TOTP changes; revokes older sessions |
| `totp_enabled BOOL` | whether login requires a TOTP second factor |
| `created_at`, `updated_at` | timestamps |

The core schema has no scoping column: accounts are global. An optional
feature that scopes accounts adds its column in its own migration.

Related tables: `user_totp` (AES-256-GCM encrypted secret, verified flag,
last used counter), `user_backup_codes` (SHA-256 hashes, `used_at`, unique per
`(user_id, code_hash)`) and `webauthn_credentials`.

`make migrate-up` applies everything. The WebAuthn and TOTP tables are inert
until their feature is enabled (`WEBAUTHN_ENABLED`, `AUTH_TOTP_ENABLED`).

Queries live in `db/queries/auth_users.sql` and `db/queries/auth_mfa.sql`;
regenerate with `make sqlc-generate`. Never edit `internal/core/db/sqlcgen/`.

## 3. Roles and permissions

`internal/core/auth/roles.go` is the registry goAuth is built with:

```go
const PermissionWhoAmI = "system.whoami"

var DefaultPermissions = []string{PermissionWhoAmI}

var DefaultRoles = map[string][]string{
    RoleUser:  {PermissionWhoAmI},
    RoleAdmin: {PermissionWhoAmI},
}
```

- Add permissions to `DefaultPermissions` and grant them to roles in
  `DefaultRoles`. goAuth encodes them as a bitmask in the access token.
- Protect routes with `policy.RequirePerm("projects.write")` or
  `policy.RequireAnyPerm(...)` after `policy.AuthRequired(...)`.
- New accounts get `Account.DefaultRole` (`user`, set in `config.go`). Public
  registration never accepts a role from the client; use `make user role=…`
  for privileged accounts.
- Changing a user's role in the database takes effect on their next login;
  bump `account_version` (or disable/enable the account through goAuth) to
  revoke existing sessions immediately.

## 4. Customizing auth behavior

Edit `internal/core/auth/config.go` (token TTLs, reset/verification strategy,
production mode, session hardening). Feature toggles are env flags; see
[auth-flows.md](auth-flows.md) and [auth-goauth.md](auth-goauth.md).
