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
- With `TENANCY_ENABLED=true`, `tenant=acme` is required; `create_tenant=1`
  creates the tenant row if it does not exist.
- goAuth's password policy applies; a duplicate email is reported as such.

Direct invocation: `go run ./cmd/createuser --help`. Container image:
`docker run --rm -it --env-file .env <image> /app/createuser --email … --role admin`.

## 2. The users schema

Migrations `000003_auth_users`, `000005_users_tenant` and `000006_auth_mfa`
(mirrored in `db/schema/auth_users.sql` and `db/schema/auth_mfa.sql`):

| Column | Purpose |
|---|---|
| `id UUID` | user id (goAuth `UserID`) |
| `email TEXT UNIQUE` | login identifier (globally unique by default; see [multi-tenancy.md](multi-tenancy.md#6-identifier-uniqueness-a-schema-decision)) |
| `password_hash TEXT` | Argon2id hash produced by goAuth |
| `role TEXT` | role name from `roles.go` |
| `permissions BIGINT` | reserved (permissions come from the role registry) |
| `status TEXT` | `active`, `pending_verification`, `disabled`, `locked`, `deleted` |
| `tenant_id TEXT` | owning tenant; `'0'` is goAuth's default tenant |
| `account_version INT` | advanced on status/TOTP changes; revokes older sessions |
| `totp_enabled BOOL` | whether login requires a TOTP second factor |
| `created_at`, `updated_at` | timestamps |

Related tables: `user_totp` (AES-256-GCM encrypted secret, verified flag,
last used counter), `user_backup_codes` (SHA-256 hashes, `used_at`),
`webauthn_credentials` (migration 000004), `tenants` (000002).

All migrations are applied by `make migrate-up`. The WebAuthn, tenancy and MFA
tables are inert until their feature is enabled.

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
