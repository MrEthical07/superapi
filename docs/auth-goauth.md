# Auth and goAuth Integration

SuperAPI's authentication engine is **[goAuth](https://github.com/MrEthical07/goAuth)
v0.8.2** (the version pinned in `go.mod`). This page explains how the template
wires it. goAuth's own reference docs are linked, pinned to that exact
version, rather than copied into this repository.

- Endpoints, request/response shapes, flags and error codes: [docs/auth-flows.md](auth-flows.md)
- Creating users, the users schema, roles: [docs/auth-bootstrap.md](auth-bootstrap.md)
- Optional features and the hooks they use: [docs/architecture.md](architecture.md#13-optional-features)
- WebAuthn: [docs/enabling-webauthn.md](enabling-webauthn.md)

## 1. goAuth reference (pinned to v0.8.2)

Base: <https://github.com/MrEthical07/goAuth/tree/v0.8.2/docs>

| Topic | goAuth page | Why you would read it |
|---|---|---|
| Config reference | [config.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/config.md) | every field set in `internal/core/auth/config.go` |
| Config lint codes | [config_lint.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/config_lint.md) | startup `goauth config lint` warnings |
| Engine API | [api-reference.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/api-reference.md) | method signatures the auth module calls |
| Flows | [flows.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/flows.md) | login, refresh, MFA, reset, verification sequences |
| MFA / TOTP / backup codes | [mfa.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/mfa.md) | TOTP setup, replay protection, backup codes |
| Password reset | [password_reset.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/password_reset.md) | strategies (token/OTP/UUID), limits |
| Email verification | [email_verification.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/email_verification.md) | challenge format, enumeration resistance |
| Sessions | [session.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/session.md) | remember-me, ceilings |
| JWT and key rotation | [jwt.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/jwt.md) | `AUTH_KEY_ID` / `AUTH_VERIFY_KEYS` |
| Rate limiting | [rate_limiting.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/rate_limiting.md) | the abuse limiters that protect public auth routes |
| WebAuthn | [webauthn.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/webauthn.md) | ceremonies and the credential provider |
| Error model | [error-model.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/error-model.md) | error categories mapped to HTTP statuses |
| Audit | [audit.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/audit.md) | audit events (never show them to end users) |
| Migrations | [migrations.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/migrations.md) | upgrade notes between goAuth versions |

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
| `AUTH_LOGIN_IP_LIMITER_ENABLED` / `_MAX_ATTEMPTS` / `_COOLDOWN` | `Security.LoginIPLimiter` (off by default; see section 7) |
| `AUTH_REFRESH_RECHECK_USER` | `Security.RecheckUserOnRefresh` (off by default; see section 7) |
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
- `CompareAndSwapPasswordHash`, `UpdateAccountStatusIf`, `UpdatePasswordHash`,
  `UpdateAccountStatus` and the MFA queries are keyed by
  user UUID. goAuth resolves the user through the lookup first, so the core SQL
  does not re-scope them; a feature that scopes accounts scopes the few calls
  goAuth makes without that lookup in its own provider decorator.

## 7. goAuth v0.8.x

SuperAPI v0.12.0 moves from goAuth v0.7.0 to **v0.8.2**. goAuth v0.8.0 is
retracted (MFA-login TOTP guessing was not limited per account); go straight to
v0.8.2. goAuth reports no incompatible API change since v0.7.0, so the upgrade
needed no code change to compile. Read goAuth's
[migrations.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/migrations.md)
and its [CHANGELOG](https://github.com/MrEthical07/goAuth/blob/v0.8.2/CHANGELOG.md)
for the full list.

### What SuperAPI adopts

**Compare-and-swap provider capabilities.** `StoreUserProvider` implements two
optional goAuth interfaces, detected at `Build`:

| Interface | Used by goAuth for | SQL (`db/queries/auth_users.sql`) |
|---|---|---|
| `goauth.PasswordHashCASUpdater` | rehash-on-login (`CompareAndSwapPasswordHash`) | `UPDATE users SET password_hash = $new, updated_at = NOW() WHERE id = $1 AND password_hash = $expected` |
| `goauth.AccountStatusCASUpdater` | email-verification activation (Pending to Active) and automatic lockout (Active to Locked) | `UPDATE users SET status = $next, account_version = account_version + 1, updated_at = NOW() WHERE id = $1 AND status = $expected` |

Both queries are `:execrows`: one affected row means applied. A password change
that lands between a login's read and its rehash write is no longer
overwritten, a verification in flight cannot re-activate an account an
administrator disabled, and a disable cannot be overwritten with `locked`. A
changed hash or status, an unknown user and a malformed id are all
`(false, nil)`; other errors pass through. The status query has exactly the
side effects of `UpdateAuthUserStatus` (status, `account_version + 1`,
`updated_at`). The scope argument goAuth passes is ignored by the core provider.
An optional feature that scopes accounts must override both with scoped
versions, or the id-keyed ones apply. No migration is needed: the queries use
existing columns.

**Two opt-in flags, both off by default** (default behavior is unchanged):

| Env var | goAuth field | Default |
|---|---|---|
| `AUTH_LOGIN_IP_LIMITER_ENABLED` | `Security.LoginIPLimiter.Enabled` | `false` |
| `AUTH_LOGIN_IP_LIMITER_MAX_ATTEMPTS` | `Security.LoginIPLimiter.MaxAttempts` | goAuth's `20` |
| `AUTH_LOGIN_IP_LIMITER_COOLDOWN` | `Security.LoginIPLimiter.Cooldown` | goAuth's `15m` |
| `AUTH_REFRESH_RECHECK_USER` | `Security.RecheckUserOnRefresh` | `false` |

An invalid value (a boolean that is not one, a non-positive integer or
duration) fails startup with a message naming the variable.

*Per-IP login limiter.* Each password login claims one failure slot for the
client address before the password is checked, across accounts, and gives it
back when the password is right; an exhausted address gets 429
(`ErrLoginRateLimited`). Only `POST /api/v1/auth/login` evaluates a password to
sign in, and it is the endpoint the limiter protects. Every auth handler
attaches the client address through `requestContext`
(`internal/modules/auth/handler.go`, `goauth.WithClientIP`) using the address
that `internal/core/httpx`'s trusted-proxy logic resolved: login, MFA confirm
(`POST /api/v1/auth/mfa/confirm`; the budget was claimed at the password step),
register (with `AUTH_REGISTRATION_AUTO_LOGIN`, which creates the session without
a password check, so the limiter does not count it), refresh, password change,
the verify-password step-up and the WebAuthn endpoints. Two things to know
before you turn it on:

- The address is only as trustworthy as `HTTP_TRUSTED_PROXIES`. A request from a
  peer that is not a trusted proxy is counted under its own address whatever
  `X-Forwarded-For` says (`TestLoginIPLimiterIgnoresForwardedHeaderFromUntrustedPeer`);
  a proxy list that is too broad lets a caller choose its address and walk away
  from the budget. A login with no address on its context is not counted.
- Users behind one NAT or office proxy share a budget, so a limit that is right
  for one site can lock out another. IPv6 addresses are grouped by /64.

*Refresh user recheck.* With `AUTH_REFRESH_RECHECK_USER=true`, each refresh reads
the user once from Postgres before it rotates the token. A disabled, locked or
deleted account, or a changed `account_version` (which advances on every status
change and TOTP enable/disable), ends the session at its next refresh, instead
of living until the refresh token expires. Costs and effects:

- one extra Postgres read per refresh;
- a Postgres outage makes refresh fail with **503** (`authentication
  unavailable`) instead of succeeding, and the refresh token is not spent;
- the template does not store goAuth's role or permission versions, so editing a
  user's `role` or `permissions` in SQL is **not** noticed by the recheck. Bump
  `account_version` in the same statement, or call
  `Engine.InvalidateUserSessions`, when a change must reach existing sessions.

Authenticated requests (`AuthRequired`) are unchanged: the recheck runs on
refresh only.

### Behavior you will see with the default configuration

These are goAuth changes, not SuperAPI ones; none needs a code change.

- A correct password sent after `MaxLoginAttempts` wrong ones is refused with
  429 until the window passes (v0.7.0 accepted it).
- MFA login: wrong TOTP codes also count against the per-user TOTP budget (5 per
  minute) across challenges; the fifth wrong code destroys the challenge and
  answers 429.
- Login reads the account twice: **one extra user read per login (by primary
  key); authenticated requests and refresh are unchanged.**
- Rehash-on-login runs after the session is issued, and now through
  `CompareAndSwapPasswordHash`.
- Only the newest password-reset or email-verification code is valid, and
  changing a password invalidates outstanding reset codes.
- A malformed challenge ID is refused before any Redis command or audit copy.
- `Config.Lint()` raises two INFO notes on every default config
  (`login_ip_limiter_disabled`, `refresh_user_recheck_off`); they are logged at
  info level. A default config also logs one WARN, `audit_disabled`, because the
  template wires no audit sink (audit events must never reach users or admins).

**Backup codes.** goAuth now issues backup codes only right after TOTP setup:
`GenerateBackupCodes` works without a TOTP code only in the 10 minutes after a
successful `ConfirmTOTPSetup`, and once. The template's `mfa/totp/confirm`
endpoint does exactly that. Afterwards the only way to get a new set is
`POST /api/v1/auth/mfa/backup-codes/regenerate` with a current TOTP code;
without one it answers 409 `backup codes must be regenerated with a current TOTP
code`. Regeneration for a user without TOTP is 409 `totp is not enabled for
this account`, and `DisableTOTP` removes the user's backup codes.

**Errors.** goAuth v0.8 adds error sentinels; the ones a template endpoint can
return map like this: `ErrTOTPRateLimited` and `ErrSessionLimitExceeded` are 429
on credential endpoints, `ErrPasswordNotSet` is 403, and
`ErrBackupCodeRegenerationRequiresTOTP` is the 409 above. The session-profile,
external sign-in and email-change sentinels cannot be returned by any template
route; when you wire those features, map the validation-category ones
(`ErrEmailChangeInvalid`, `ErrExternalTokenInvalid`, `ErrExternalSignupInvalid`,
`ErrExternalProviderUnknown`, `ErrUnknownSessionProfile`,
`ErrSessionProfileConflict`) in `internal/modules/auth/errors.go`, because
unmapped they fall back to 401.

### Available in goAuth, not wired in the template

None of these has an endpoint, a flag or a provider implementation here, so none
of their behavior is present.

- **Session profiles** (`Config.SessionProfiles`, `WithSessionProfile`): named
  lifetime and access-token policies, so one engine can issue a short web
  session and a long mobile one. To add them, define the profiles in
  `config.go`, choose the profile on the server side before the login call
  (never from request input), and map `ErrUnknownSessionProfile` and
  `ErrSessionProfileConflict`. Note that `remember_me` then needs a
  `RememberMeProfile`, or the login is refused. See goAuth's
  [session.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/session.md).
- **External sign-in** (Google, Sign in with Apple, OIDC;
  `Engine.LoginWithExternalIdentity`): the application obtains the ID token, goAuth
  verifies it and issues a normal session. To add it, set
  `Config.ExternalAuth`, implement the `ExternalIdentityProvider` capability on
  the user provider (a table for linked identities, so a migration), add the
  endpoints and map its sentinels. See
  [external_identity.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/external_identity.md).
  Accounts created this way have no password, which is what `ErrPasswordNotSet`
  reports.
- **Change email** (`Config.EmailChange`, `Engine.RequestEmailChange` and
  `ConfirmEmailChange`): a signed-in user proves ownership of a new address. To
  add it, enable the config, add request and confirm endpoints behind a
  step-up, deliver the code through `internal/core/notify`, and map its
  sentinels. See
  [email_change.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/email_change.md).
- **Test clock** (`Builder.WithClock`): lets a test move the time the engine
  reads, to walk a session to its idle limit or a challenge to its expiry. The
  template builds the engine in `internal/core/auth/goauth_provider.go`; pass a
  clock through the builder there from a test-only constructor. See
  [testing.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/testing.md).

Redis: goAuth needs 6.0 or later, and **WebAuthn needs 6.2 or later** (it reads
its ceremony session with `GETDEL`). The template's `docker-compose.yml` ships
Redis 8.

## 8. Security rules for extensions

- Never return reset or verification secrets in an HTTP response; deliver them
  through `notify.Notifier`.
- Keep request endpoints enumeration-safe: the same status and body whether or
  not the account exists.
- Never accept a role from client input on public endpoints.
- Never expose goAuth audit events to end users or your own product admins; the audit
  stream distinguishes cases responses deliberately hide.
- Compare secrets you handle yourself with `crypto/subtle`.

## 9. Available in goAuth, not wired in the template

Session profiles, external sign-in, change email and the test clock are listed
in section 7. The rest:

- **Role switching** (`Engine.SwitchRole`, the optional `RoleSwitchProvider`
  capability, `Config.RoleSwitch`; goAuth v0.7.0). One session changes which of
  an account's roles it acts as, without a new login. The template does not
  enable it, offers no endpoint for it and implements no `RoleSwitchProvider`,
  so none of its behavior is present. See goAuth's
  [role_switching.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/role_switching.md).

  It is only safe on `ModeStrict` routes. After a switch, a strict route rejects
  the old access token on its next use, but `hybrid` and `jwt_only` routes keep
  accepting it, with its old role mask, until it expires. The template's
  `AuthRequired` uses the configured mode (`AUTH_MODE`, `hybrid` by default), so
  a project that adopts role switching must run every route that gates on role
  or permissions in strict mode: set `AUTH_MODE=strict`, or build those routes
  with `auth.ModeStrict`.

## 10. Troubleshooting

| Symptom | Check |
|---|---|
| Startup: goAuth rejects the provider for a missing optional interface | a custom provider replaced `StoreUserProvider` (or a feature's decorator) without the optional goAuth interfaces it implements |
| Startup: `AUTH_TOTP_ENABLED requires AUTH_TOTP_ENCRYPTION_KEY` | generate one: `openssl rand -base64 32` |
| Startup: `AUTH_TEST_SHARED_SECRET is only allowed with APP_ENV=dev or APP_ENV=test` | remove `AUTH_TEST_*` outside dev/test |
| Login 401 for a user that exists | wrong password, or a scope an optional feature requires is missing or wrong |
| Login 403 `authentication state rejected` | account pending verification, disabled or locked |
| Every protected route 401 | token sent as `Authorization: Bearer …`? expired? a feature's auth extension rejecting it? |
| `goauth config lint` warnings at startup | see goAuth [config_lint.md](https://github.com/MrEthical07/goAuth/blob/v0.8.2/docs/config_lint.md); only high severity fails startup |
