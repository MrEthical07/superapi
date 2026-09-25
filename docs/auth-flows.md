# Auth Flows and Endpoints

Every auth endpoint lives in `internal/modules/auth` under `/api/v1/auth`
(handler -> service -> goAuth engine). This page lists each endpoint, the flag
that enables it, request/response shapes, error codes, and the
enumeration-safety guarantees.

> Moved in v0.9.0: the demo paths `/api/v1/system/auth/*` and
> `/api/v1/system/whoami` are now `/api/v1/auth/*` and `/api/v1/auth/whoami`.

## Conventions

- JSON in, JSON out, wrapped in the standard envelope:
  `{"ok": true, "data": {…}, "request_id": "…"}` or
  `{"ok": false, "error": {"code": "…", "message": "…"}, "request_id": "…"}`.
- Unknown JSON fields are rejected (400), so a client cannot sneak in `role`.
- With `TENANCY_ENABLED=true` every request (except `/healthz`, `/readyz`,
  `/metrics`) needs a tenant (default header `X-Tenant-ID`); see
  [multi-tenancy.md](multi-tenancy.md).
- Endpoint groups that are disabled are **not registered**: they return 404
  like any unknown route. With `AUTH_ENABLED=false` no auth route exists.
- Public endpoints are protected by goAuth's built-in abuse limiters (per
  tenant + identifier and IP). The handler passes client IP and User-Agent to
  goAuth. `whoami` and `password/change` also get the route rate limiter when
  `RATELIMIT_ENABLED=true`.

## Feature flags

| Flag | Default | Endpoints |
|---|---|---|
| `AUTH_ENABLED` | false | login, mfa/confirm, refresh, logout, logout/all, sessions, password/change, whoami, webauthn/* |
| `AUTH_REGISTRATION_ENABLED` | false | register |
| `AUTH_PASSWORD_RESET_ENABLED` | false | password/reset/request, password/reset/confirm |
| `AUTH_EMAIL_VERIFICATION_ENABLED` | false | email/verify/request, email/verify/confirm |
| `AUTH_TOTP_ENABLED` | false | mfa/totp/setup, mfa/totp/confirm, mfa/totp/disable, mfa/backup-codes/regenerate |
| `WEBAUTHN_ENABLED` | false | webauthn/* return 403 until enabled ([enabling-webauthn.md](enabling-webauthn.md)) |

Related: `AUTH_REGISTRATION_AUTO_LOGIN`, `AUTH_EMAIL_VERIFICATION_REQUIRED`,
`AUTH_TOTP_ENCRYPTION_KEY`, `AUTH_TOTP_ISSUER`, `NOTIFY_*` — see
[environment-variables.md](environment-variables.md).

## Error codes

| HTTP | `error.code` | When |
|---|---|---|
| 400 | `bad_request` | missing/oversized fields, unknown fields, invalid or expired challenge, invalid code, password policy/reuse, wrong current password |
| 400 | `bad_request` `tenant required` / `tenant invalid` | tenancy on and no/malformed tenant |
| 401 | `unauthorized` | bad credentials, bad refresh/access token, token from another tenant |
| 403 | `forbidden` `authentication state rejected` | account pending verification, disabled, locked |
| 404 | `not_found` | feature disabled; tenant unknown or inactive |
| 409 | `conflict` | TOTP already enabled (setup) or not enabled (disable/regenerate) |
| 429 | `too_many_requests` | goAuth abuse limiter or route rate limiter |
| 503 | `dependency_unavailable` | Redis/Postgres/goAuth backend unavailable |

## Credentials

### POST /api/v1/auth/login

```json
{"identifier": "alice@example.com", "password": "…", "remember_me": false}
```

200 with tokens:

```json
{"access_token": "…", "refresh_token": "…", "access_expires_utc": "2026-01-01T00:05:00Z", "access_expires_unix": 1767225900}
```

or, when the account has TOTP (or WebAuthn) and a second factor is required,
200 with a challenge and no tokens:

```json
{"mfa_required": true, "mfa_challenge": "…", "mfa_type": "totp", "mfa_types": ["totp"]}
```

Wrong password, unknown user and (with tenancy) a user from another tenant all
return the same 401 `invalid credentials`.

### POST /api/v1/auth/mfa/confirm

```json
{"challenge": "…", "code": "123456", "type": "totp"}
```

`type` is `totp` (default), `backup`, or `webauthn`. Returns the token shape.
A TOTP code is accepted once (replay protection); a backup code is single-use.

### POST /api/v1/auth/refresh

`{"refresh_token": "…"}` -> token shape. Refresh tokens rotate; reuse of an old
one is detected by goAuth.

### POST /api/v1/auth/logout

`{"access_token": "…"}` or `Authorization: Bearer …` -> `{"logged_out": true}`.
Accepts an expired-but-authentic token.

## Account (always on, authenticated)

| Endpoint | Body | Response |
|---|---|---|
| `GET /api/v1/auth/whoami` | — | `{"user_id","tenant_id","role","permissions"}` |
| `GET /api/v1/auth/sessions` | — | `{"sessions":[{"session_id","created_utc","expires_utc"}]}` |
| `POST /api/v1/auth/logout/all` | — | `{"logged_out": true}`; revokes every session of the user |
| `POST /api/v1/auth/password/change` | `{"current_password","new_password"}` | `{"changed": true}`; wrong current password -> 400 |

## Registration (`AUTH_REGISTRATION_ENABLED`)

### POST /api/v1/auth/register

```json
{"identifier": "new@example.com", "password": "…", "remember_me": false}
```

Always **202** `{"accepted": true}`, whether the identifier is new or already
taken. goAuth hashes the password before the duplicate is detected, so timing
is comparable. The account gets the default role (`Account.DefaultRole`,
`user`); the role can never come from the request.

With `AUTH_EMAIL_VERIFICATION_ENABLED`, the account starts
`pending_verification`, and a verification message is sent (only to a pending
account; an existing verified account gets nothing).

With `AUTH_REGISTRATION_AUTO_LOGIN=true`, a new account gets **201** with the
token shape instead. An existing identifier still gets 202, so auto-login
**trades away registration enumeration resistance**. Leave it off unless that
is acceptable for your product.

Follow-up worth adding for your product: notify the owner of an existing
address that someone tried to register it.

## Password reset (`AUTH_PASSWORD_RESET_ENABLED`)

### POST /api/v1/auth/password/reset/request

`{"identifier": "…"}` -> always **202** `{"accepted": true}`. goAuth returns a
synthetic challenge for unknown identifiers (and sleeps to equalize timing). The
real challenge is delivered through the notifier only when the account exists
(in the request tenant), asynchronously so response time does not depend on it.
The challenge is **never** in the response. Rate limited -> 429.

### POST /api/v1/auth/password/reset/confirm

```json
{"challenge": "…", "new_password": "…", "mfa_type": "totp", "mfa_code": "123456"}
```

`mfa_type`/`mfa_code` are optional; they are required only if you enable
goAuth's `TOTP.RequireForPasswordReset` in `config.go` (off by default because
it applies to every account). Success -> `{"changed": true}` and every session
of the user is revoked. Invalid/expired/reused challenge -> 400.

## Email verification (`AUTH_EMAIL_VERIFICATION_ENABLED`)

New accounts start `pending_verification`; with
`AUTH_EMAIL_VERIFICATION_REQUIRED=true` (default) login returns 403 until
verified. Existing active accounts are unaffected.

### POST /api/v1/auth/email/verify/request

`{"identifier": "…"}` -> always **202**. Delivers a challenge only to an
account that is pending verification, the only case in which goAuth stores a
real record.

### POST /api/v1/auth/email/verify/confirm

Either the full challenge from the message:

```json
{"challenge": "tenant:verification-id:code"}
```

or its parts (preferred by goAuth, since the code is then never inside an
opaque string that might be logged):

```json
{"verification_id": "…", "code": "…"}
```

-> `{"confirmed": true}`. The full-challenge form carries its own tenant and
works regardless of the request tenant; the id+code form uses the request
tenant.

## TOTP and backup codes (`AUTH_TOTP_ENABLED`)

Requires `AUTH_TOTP_ENCRYPTION_KEY` (secrets are encrypted at rest). Users who
enroll are challenged at every login; users who do not are unaffected.

| Endpoint | Body | Response / notes |
|---|---|---|
| `POST /api/v1/auth/mfa/totp/setup` | — | `{"secret_base32","otpauth_uri"}`. Render the URI as a QR code. 409 if TOTP is already enabled (disable first; replacing an active secret would break the user's authenticator) |
| `POST /api/v1/auth/mfa/totp/confirm` | `{"code"}` | `{"enabled": true, "backup_codes": [...]}`. Backup codes are shown once; only hashes are stored. goAuth revokes the user's sessions, so log in again |
| `POST /api/v1/auth/mfa/totp/disable` | `{"code"}` | requires a current TOTP code; removes the secret and all backup codes. 409 if not enabled |
| `POST /api/v1/auth/mfa/backup-codes/regenerate` | `{"code"}` | requires a current TOTP code; returns a fresh set and invalidates the old one |

## WebAuthn

`POST /api/v1/auth/webauthn/register/begin`, `…/register/finish`,
`GET /api/v1/auth/webauthn/credentials`, `POST /api/v1/auth/webauthn/credentials/remove`.
All authenticated; see [enabling-webauthn.md](enabling-webauthn.md).

## Delivering reset and verification messages

`internal/core/notify` defines:

```go
type Notifier interface {
    SendPasswordReset(ctx context.Context, to, challenge string) error
    SendEmailVerification(ctx context.Context, to, challenge string) error
}
```

- `NOTIFY_DRIVER=noop` (default) discards messages. Reset and verification
  cannot complete until you plug in a real notifier.
- `NOTIFY_DRIVER=log` logs a redacted message; the full secret is logged only
  with `APP_ENV=dev` and `NOTIFY_LOG_SECRETS=true` (lint enforces dev).
- Production: implement `Notifier` over SMTP or your email/SMS provider (build
  a link such as `https://app.example.com/reset?challenge=…` in your template)
  and return it from `notify.New`. Calls are made asynchronously by a bounded
  dispatcher with `NOTIFY_TIMEOUT`, so they must be safe to run after the HTTP
  response is written.

## Creating the first user

Public registration is off by default. Create accounts with
`make user email=admin@example.com role=admin`; see
[auth-bootstrap.md](auth-bootstrap.md).
