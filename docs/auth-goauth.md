# Auth and goAuth Integration

This document explains how authentication works in SuperAPI with goAuth, including runtime wiring, route behavior, and data flow through the new store-first architecture.

## 1. Big Picture

SuperAPI uses goAuth as the auth engine.

The integration boundary did not change:

- goAuth still receives a goauth.UserProvider

What changed:

- user persistence now goes through repository + store layers

Current path:

StoreUserProvider -> UserRepository -> RelationalStore -> Postgres

## 2. Key Files

- internal/core/auth/goauth_provider.go
	- builds goAuth engine and maps auth mode
- internal/core/auth/provider_sqlc.go
	- StoreUserProvider implementation used by goAuth
- internal/core/auth/user_repository.go
	- repository implementation over relational store
- internal/core/storage/postgres_store.go
	- relational execution and transaction behavior
- internal/core/app/deps.go
	- startup wiring of provider/repository/store/engine
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
