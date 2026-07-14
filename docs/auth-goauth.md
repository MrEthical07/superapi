# Auth and goAuth Integration

This document explains how authentication works in SuperAPI with goAuth, including runtime wiring, route behavior, and data flow through the sqlc data layer.

> Migration note: SuperAPI is on goAuth **v0.4.0**. See §0 for the v0.4.0
> features wired in (remember-me, MFA-aware login, graceful logout, session
> ceiling, sliding-window limiter, Ed25519 key rotation, and WebAuthn scaffolded
> off). Some sections below still describe the pre-v0.4.0 model and are being
> migrated; the authoritative field/method names are in
> `internal/core/auth/config.go` and the system module.

## 0. goAuth v0.4.0 features

Configured in `internal/core/auth/config.go` (the customization point) and
surfaced by the system module:

- **Remember-me + session ceiling.** Login accepts `remember_me`; the absolute
  lifetime is capped by `AUTH_MAX_SESSION_DURATION`.
- **MFA-aware login.** When an account requires a second factor, login returns
  an MFA challenge (`mfa_required`, `mfa_challenge`, `mfa_type`, `mfa_types`)
  instead of tokens. Complete it at `POST /api/v1/system/auth/mfa/confirm`.
- **Graceful logout.** `POST /api/v1/system/auth/logout` revokes the session via
  `LogoutByAccessToken`, which accepts an expired-but-authentic access token.
- **Sliding-window limiter.** `AUTH_LIMITER_WINDOW_MODE=sliding` switches
  goAuth's internal auth-abuse limiter algorithm (separate from SuperAPI's route
  rate limiter).
- **Ed25519 key rotation.** `AUTH_KEY_ID` + `AUTH_VERIFY_KEYS` populate goAuth's
  `JWT.KeyID` / `JWT.VerifyKeys` for zero-downtime rotation (set both or neither).
- **WebAuthn** — scaffolded, disabled by default. See docs/enabling-webauthn.md.

Endpoints (system module): `login`, `mfa/confirm`, `refresh`, `logout`,
`whoami`, and `webauthn/*`. Login/refresh/logout/MFA flow through a thin
`authService` (handler -> service), matching the enforced architecture.

## 1. Big Picture

SuperAPI uses goAuth as the auth engine.

The integration boundary did not change:

- goAuth still receives a goauth.UserProvider

Data path (sqlc data layer):

StoreUserProvider -> UserRepository -> storage.Postgres (sqlc queries) -> pgx

## 2. Key Files

- internal/core/auth/goauth_provider.go
	- builds goAuth engine and maps auth mode
- internal/core/auth/config.go
	- goAuth configuration customization point (v0.4.0 features)
- internal/core/auth/provider_store.go
	- StoreUserProvider (UserProvider + WebAuthnCredentialProvider)
- internal/core/auth/user_repository.go
	- auth repository over the sqlc storage boundary
- internal/core/auth/webauthn_repository.go
	- WebAuthn credential repository (optional; used when WebAuthn is enabled)
- internal/core/storage/postgres_store.go
	- sqlc query boundary and transaction behavior
- internal/core/app/deps.go
	- startup wiring of provider/repository/engine
- internal/modules/system/service.go
	- thin auth service (login/refresh/logout/MFA/WebAuthn ceremonies)
- internal/core/policy/auth.go
	- route auth policy behavior and context injection

## 3. Startup Wiring Flow

Auth wiring is performed during dependency initialization.

Sequence when auth is enabled:

1. Postgres pool is initialized.
2. Relational store is created from pool.
3. UserRepository is created from relational store.
4. StoreUserProvider is created from repository.
5. goAuth engine is built with redis + provider.

If required dependencies are missing, startup fails fast.

## 4. Runtime Requirements

From config lint behavior:

- AUTH_ENABLED=true requires REDIS_ENABLED=true
- AUTH_ENABLED=true requires POSTGRES_ENABLED=true

Reason:

- goAuth uses Redis-backed session behavior
- current provider persistence path uses relational store over Postgres

## 5. Auth Modes

AUTH_MODE values:

- jwt_only
- hybrid
- strict

Mode parsing and mapping happen in internal/core/auth/goauth_provider.go.

At route level, policy.AuthRequired(engine, mode) enforces the selected mode.

## 6. Route Protection Behavior

Auth policy implementation is in internal/core/policy/auth.go.

AuthRequired behavior:

- validates bearer token through goAuth middleware guard
- returns 401 for missing/invalid auth
- injects auth.AuthContext into request context

AuthContext includes:

- UserID
- TenantID
- Role
- Permissions

That context is then consumed by downstream handlers and tenant/RBAC policies.

## 7. Auth Routes In System Module

Routes are registered in internal/modules/system/routes.go.

### 7.1 POST /api/v1/system/auth/login

Flow:

1. handler validates identifier/password
2. handler calls goAuth engine Login
3. goAuth uses StoreUserProvider for user lookup
4. provider calls UserRepository
5. repository executes relational operation via store
6. store executes against pgx runner
7. goAuth validates and issues tokens

### 7.2 POST /api/v1/system/auth/refresh

Flow:

1. handler validates refresh token
2. handler calls goAuth engine Refresh
3. goAuth validates token/session state
4. provider/repository/store path is used when user persistence lookup is needed

### 7.3 GET /api/v1/system/whoami

Flow:

1. AuthRequired validates request and injects auth context
2. optional rate-limit policy runs
3. handler returns principal data from context

This route demonstrates policy-driven auth context usage.

## 8. UserProvider Compatibility

StoreUserProvider methods cover goAuth user-provider needs:

- GetUserByIdentifier
- GetUserByID
- UpdatePasswordHash
- CreateUser
- UpdateAccountStatus
- TOTP/backup-code methods (currently stubs)

Mapping behavior:

- repository not-found error is translated to goauth.ErrUserNotFound
- storage projection is mapped to goauth.UserRecord

Result:

- goAuth contract remains compatible
- persistence internals are architecture-compliant

## 9. Why The Store-Backed Provider Matters

Benefits:

- auth persistence follows same architecture as modules
- no direct query-object coupling in provider construction
- easier future backend evolution behind store contracts
- cleaner testability at repository and provider boundaries

## 10. Common Integration Mistakes

Mistake: implementing custom token parsing in module handlers

- Correct approach: use AuthRequired policy and auth context extraction

Mistake: bypassing provider/repository to hit DB directly for auth user reads

- Correct approach: keep all auth persistence in auth repository + store path

Mistake: putting RBAC checks before auth policy

- Correct approach: attach policies in correct order (auth before tenant/RBAC)

## 11. Troubleshooting Checklist

Login returns 401:

- confirm AUTH_ENABLED=true
- confirm Redis and Postgres are enabled
- verify identifier/password input

All protected routes return 401:

- confirm auth policy attached to routes correctly
- verify token is sent as Bearer token
- verify auth mode and engine startup logs

Auth startup fails:

- check config lint output for missing required env vars
- check Redis/Postgres connectivity and startup ping timeouts

## 12. Related References

- [docs/architecture.md](architecture.md)
- [docs/policies.md](policies.md)
- [docs/auth-bootstrap.md](auth-bootstrap.md)
- [docs/environment-variables.md](environment-variables.md)
