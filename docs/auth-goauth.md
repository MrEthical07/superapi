# Auth and goAuth Integration

SuperAPI's authentication engine is **[goAuth](https://github.com/MrEthical07/goAuth)
v0.7.0** (the version pinned in `go.mod`). This page explains how the template
wires it. goAuth's own reference docs are linked, pinned to that exact
version, rather than copied into this repository.

- Endpoints, request/response shapes, flags and error codes: [docs/auth-flows.md](auth-flows.md)
- Creating users, the users schema, roles: [docs/auth-bootstrap.md](auth-bootstrap.md)
- Optional features and the hooks they use: [docs/architecture.md](architecture.md#13-optional-features)
- WebAuthn: [docs/enabling-webauthn.md](enabling-webauthn.md)

## 1. goAuth reference (pinned to v0.7.0)

Base: <https://github.com/MrEthical07/goAuth/tree/v0.7.0/docs>

| Topic | goAuth page | Why you would read it |
|---|---|---|
| Config reference | [config.md](https://github.com/MrEthical07/goAuth/blob/v0.7.0/docs/config.md) | every field set in `internal/core/auth/config.go` |
| Config lint codes | [config_lint.md](https://github.com/MrEthical07/goAuth/blob/v0.7.0/docs/config_lint.md) | startup `goauth config lint` warnings |
| Engine API | [api-reference.md](https://github.com/MrEthical07/goAuth/blob/v0.7.0/docs/api-reference.md) | method signatures the auth module calls |
| Flows | [flows.md](https://github.com/MrEthical07/goAuth/blob/v0.7.0/docs/flows.md) | login, refresh, MFA, reset, verification sequences |
| MFA / TOTP / backup codes | [mfa.md](https://github.com/MrEthical07/goAuth/blob/v0.7.0/docs/mfa.md) | TOTP setup, replay protection, backup codes |
| Password reset | [password_reset.md](https://github.com/MrEthical07/goAuth/blob/v0.7.0/docs/password_reset.md) | strategies (token/OTP/UUID), limits |
| Email verification | [email_verification.md](https://github.com/MrEthical07/goAuth/blob/v0.7.0/docs/email_verification.md) | challenge format, enumeration resistance |
| Sessions | [session.md](https://github.com/MrEthical07/goAuth/blob/v0.7.0/docs/session.md) | remember-me, ceilings |
| JWT and key rotation | [jwt.md](https://github.com/MrEthical07/goAuth/blob/v0.7.0/docs/jwt.md) | `AUTH_KEY_ID` / `AUTH_VERIFY_KEYS` |
| Rate limiting | [rate_limiting.md](https://github.com/MrEthical07/goAuth/blob/v0.7.0/docs/rate_limiting.md) | the abuse limiters that protect public auth routes |
| WebAuthn | [webauthn.md](https://github.com/MrEthical07/goAuth/blob/v0.7.0/docs/webauthn.md) | ceremonies and the credential provider |
| Error model | [error-model.md](https://github.com/MrEthical07/goAuth/blob/v0.7.0/docs/error-model.md) | error categories mapped to HTTP statuses |
| Audit | [audit.md](https://github.com/MrEthical07/goAuth/blob/v0.7.0/docs/audit.md) | audit events (never show them to end users) |
| Migrations | [migrations.md](https://github.com/MrEthical07/goAuth/blob/v0.7.0/docs/migrations.md) | upgrade notes between goAuth versions |

When you bump goAuth, update the version in these links together with `go.mod`.

## 2. Where things live

| Concern | File |
|---|---|
| goAuth config (the customization point) | `internal/core/auth/config.go` |
| Roles and permissions | `internal/core/auth/roles.go` |
| Engine construction | `internal/core/auth/goauth_provider.go` |
| Core user provider (`UserProvider`, TOTP/backup codes; scope-free) | `internal/core/auth/provider_store.go` |
| WebAuthn provider methods | `internal/core/auth/provider_webauthn.go`, `webauthn_repository.go` |
| Users repository (sqlc) | `internal/core/auth/user_repository.go` |
| TOTP / backup-code repository (sqlc) | `internal/core/auth/mfa_repository.go` |
| TOTP secret encryption at rest | `internal/core/auth/secret_cipher.go` |
| Feature hooks (provider decorator, goAuth config mutator, auth extension) | `internal/core/app/feature.go`, `internal/features/features.go` |
| HTTP endpoints (handler -> service -> engine) | `internal/modules/auth/` |
| Reset / verification delivery | `internal/core/notify/` |
| Route auth policy | `internal/core/policy/auth.go` |
| Startup wiring | `internal/core/app/deps.go` |
| First user / ops accounts | `cmd/createuser` (`make user`) |

Data path, identical to every other module:

```
goAuth engine -> StoreUserProvider -> UserRepository / MFARepository
              -> storage.Postgres.Queries(ctx) (sqlc) -> pgx
```

## 3. Startup wiring

With `AUTH_ENABLED=true` (requires `POSTGRES_ENABLED` and `REDIS_ENABLED`),
`internal/core/app/deps.go`:

1. builds `UserRepository` over the `storage.Postgres` boundary;
2. builds `StoreUserProvider` with the WebAuthn repository;
3. when `AUTH_TOTP_ENABLED=true`, attaches the MFA repository and the
   AES-256-GCM cipher keyed by `AUTH_TOTP_ENCRYPTION_KEY`;
4. lets each registered optional feature (`internal/features/features.go`)
   contribute through `app.Hooks`: a `UserProvider` decorator that wraps the
   core provider, `GoAuthConfig` mutators, and a `policy.AuthExtension`;
5. calls `auth.NewGoAuthEngine(redis, mode, features, provider, mutators...)`,
   which runs `ProjectGoAuthConfig`, the mutators, goAuth's config lint
   (high-severity findings fail startup) and `Builder.Build()`, then registers
   the extensions with `policy.UseAuthExtensions(engine, ...)`.

`cmd/createuser` calls the same `app.NewDependencies`, so tools build an
identical engine.
<!-- template:begin perf -->
`cmd/perftoken` (load tests) does too.
<!-- template:end perf -->

## 4. Configuration map

`ProjectGoAuthConfig(mode, features, mutators...)` starts from
`goauth.DefaultConfig()`, applies the table below, then runs the feature
mutators (`auth.ConfigMutator`, one per feature that needs goAuth settings of
its own):

| SuperAPI setting | goAuth field(s) |
|---|---|
| `AUTH_MODE` | `ValidationMode` |
| `AUTH_REGISTRATION_AUTO_LOGIN` | `Account.AutoLogin` (`Account.Enabled` is always on so `make user` works) |
| `AUTH_PASSWORD_RESET_ENABLED` | `PasswordReset.Enabled` |
| `AUTH_EMAIL_VERIFICATION_ENABLED` / `_REQUIRED` | `EmailVerification.Enabled` / `RequireForLogin` |
| `AUTH_TOTP_ENABLED` / `AUTH_TOTP_ISSUER` | `TOTP.Enabled`, `TOTP.RequireForLogin` (challenges enrolled users), `TOTP.Issuer` |
| `AUTH_MAX_SESSION_DURATION` | `Session.MaxSessionDuration` |
| `AUTH_LIMITER_WINDOW_MODE` | `Security.LimiterWindowMode` |
| `AUTH_KEY_ID` / `AUTH_VERIFY_KEYS` | `JWT.KeyID` / `JWT.VerifyKeys` (set both or neither) |
| `WEBAUTHN_*` | `WebAuthn.*` |
| `AUTH_TEST_*` (dev/test only) | HS256 shared secret and TTLs for perf runs |

Everything else keeps goAuth's defaults. Edit `config.go` for project-specific
choices (token TTLs, reset/verification strategy, `Security.ProductionMode`, …).

## 5. Route protection

`policy.AuthRequired(engine, mode)` validates the bearer token through goAuth's
middleware guard, returns 401 on failure, and injects `auth.AuthContext`
(`UserID`, `Role`, `Permissions`, `Attributes`) for downstream policies and
handlers. Optional features take part through a `policy.AuthExtension`: it adds
principal attributes (read with `principal.Attribute(key)`) and can run a
post-authentication check that rejects the request with the same 401. See
[policies.md](policies.md). Never parse tokens in module code.

## 6. Provider behavior worth knowing

- **Scope-free core.** The core provider looks users up by identifier or id
  with no extra scoping. A feature that scopes accounts wraps it with a
  `UserProvider` decorator that keeps every optional goAuth interface the core
  provider implements (goAuth detects capabilities by type assertion).
- **Duplicates.** A unique violation on `users.email` maps to
  `goauth.ErrProviderDuplicateIdentifier`. goAuth reports `ErrAccountExists`
  after hashing the password, so duplicate and fresh registrations take similar
  time.
- **Account version.** `users.account_version` advances on every status change
  and TOTP enable/disable. goAuth refuses transitions that do not advance it,
  and it revokes sessions stamped with an older version.
- **TOTP secrets** are encrypted with AES-256-GCM before storage, bound to the
  user id. goAuth hands the provider the raw secret; it does not encrypt it.
  Rotating `AUTH_TOTP_ENCRYPTION_KEY` makes existing secrets undecryptable
  (users must re-enroll). Key rotation support is a listed follow-up.
- **Replay protection** is enforced twice: goAuth compares counters, and the
  SQL update only moves `last_used_counter` forward.
- **Backup codes** are SHA-256 hashes, consumed with one `UPDATE … WHERE used_at
  IS NULL`, so a code works once even under concurrent use.
- `UpdatePasswordHash`, `UpdateAccountStatus` and the MFA queries are keyed by
  user UUID. goAuth resolves the user through the lookup first, so the core SQL
  does not re-scope them; a feature that scopes accounts scopes the few calls
  goAuth makes without that lookup in its own provider decorator.

## 7. Security rules for extensions

- Never return reset or verification secrets in an HTTP response; deliver them
  through `notify.Notifier`.
- Keep request endpoints enumeration-safe: the same status and body whether or
  not the account exists.
- Never accept a role from client input on public endpoints.
- Never expose goAuth audit events to end users or your own product admins; the audit
  stream distinguishes cases responses deliberately hide.
- Compare secrets you handle yourself with `crypto/subtle`.

## 8. Available in goAuth, not wired in the template

- **Role switching** (`Engine.SwitchRole`, the optional `RoleSwitchProvider`
  capability, `Config.RoleSwitch`; goAuth v0.7.0). One session changes which of
  an account's roles it acts as, without a new login. The template does not
  enable it, offers no endpoint for it and implements no `RoleSwitchProvider`,
  so none of its behavior is present. See goAuth's
  [role_switching.md](https://github.com/MrEthical07/goAuth/blob/v0.7.0/docs/role_switching.md).

  It is only safe on `ModeStrict` routes. After a switch, a strict route rejects
  the old access token on its next use, but `hybrid` and `jwt_only` routes keep
  accepting it, with its old role mask, until it expires. The template's
  `AuthRequired` uses the configured mode (`AUTH_MODE`, `hybrid` by default), so
  a project that adopts role switching must run every route that gates on role
  or permissions in strict mode: set `AUTH_MODE=strict`, or build those routes
  with `auth.ModeStrict`.

## 9. Troubleshooting

| Symptom | Check |
|---|---|
| Startup: goAuth rejects the provider for a missing optional interface | a custom provider replaced `StoreUserProvider` (or a feature's decorator) without the optional goAuth interfaces it implements |
| Startup: `AUTH_TOTP_ENABLED requires AUTH_TOTP_ENCRYPTION_KEY` | generate one: `openssl rand -base64 32` |
| Startup: `AUTH_TEST_SHARED_SECRET is only allowed with APP_ENV=dev or APP_ENV=test` | remove `AUTH_TEST_*` outside dev/test |
| Login 401 for a user that exists | wrong password, or a scope an optional feature requires is missing or wrong |
| Login 403 `authentication state rejected` | account pending verification, disabled or locked |
| Every protected route 401 | token sent as `Authorization: Bearer …`? expired? a feature's auth extension rejecting it? |
| `goauth config lint` warnings at startup | see goAuth [config_lint.md](https://github.com/MrEthical07/goAuth/blob/v0.7.0/docs/config_lint.md); only high severity fails startup |
