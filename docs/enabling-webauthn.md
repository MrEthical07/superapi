# Enabling WebAuthn

WebAuthn is fully scaffolded but **disabled by default**. Nothing about it is
active until you enable it. This page covers what ships and how to turn it on.

## What ships (disabled)

- goAuth's WebAuthn config surface, wired from `WEBAUTHN_*` env in
  `internal/core/auth/config.go` (`WEBAUTHN_ENABLED=false` by default).
- `StoreUserProvider` implements `goauth.WebAuthnCredentialProvider`
  (`internal/core/auth/provider_webauthn.go`), backed by a WebAuthn credential
  repository over the sqlc boundary (`internal/core/auth/webauthn_repository.go`).
- Ceremony endpoints under `/api/v1/auth/webauthn/*` (register begin /
  finish, list credentials, remove credential), auth-protected. While WebAuthn
  is disabled these return a "webauthn disabled" error (403-class), so they act
  as a working, self-documenting example.
- Migration `db/migrations/000004_webauthn_credentials.up.sql` and its sqlc
  schema mirror + queries. It is **always applied** by `make migrate-up` and
  stays inert until WebAuthn is enabled.

Because goAuth only requires the WebAuthn credential capability when
`WebAuthn.Enabled` is true, shipping the provider methods and endpoints while
disabled is safe — `Build()` does not fail.

## Step 1 — make sure migrations are applied

`make migrate-up` (it applies 000004 along with every other migration; nothing
extra to do).

## Step 2 — enable via config

Set at least the required Relying Party fields:

```
WEBAUTHN_ENABLED=true
WEBAUTHN_RP_ID=example.com
WEBAUTHN_RP_DISPLAY_NAME="Example"
WEBAUTHN_RP_ORIGINS=https://app.example.com
```

Optional tuning: `WEBAUTHN_ATTESTATION_PREFERENCE`, `WEBAUTHN_USER_VERIFICATION`,
`WEBAUTHN_CEREMONY_TTL`, `WEBAUTHN_REQUIRE_FOR_LOGIN`,
`WEBAUTHN_REJECT_CLONED_AUTHENTICATORS`. See docs/environment-variables.md.

## Ceremony flow (browser)

1. `POST /api/v1/auth/webauthn/register/begin` → returns `ceremony_id`
   and `options_json`. Pass `options_json` to `navigator.credentials.create`.
2. `POST /api/v1/auth/webauthn/register/finish` with the `ceremony_id`
   and the authenticator's `response_json` → persists the credential.
3. `GET /api/v1/auth/webauthn/credentials` lists a user's credentials;
   `POST .../credentials/remove` removes one by base64url credential id.

Login assertions are completed through the MFA confirm endpoint with
`type: "webauthn"` once `WEBAUTHN_REQUIRE_FOR_LOGIN` (or per-user credentials)
brings WebAuthn into the login path. goAuth manages the single-use ceremony
state in Redis under the `awn:` prefix.

## Removing WebAuthn entirely

<!-- template:begin init -->
On a fresh clone, `make init flags=--no-webauthn` does all of this.
<!-- template:end init -->

Otherwise delete the migration (only on databases that never applied it),
schema mirror and queries (`*webauthn_credentials*`, then `make sqlc-generate`),
`internal/core/auth/webauthn_repository.go`, `provider_webauthn.go`,
`config_webauthn.go`, the `webauthnRepo` field and `WithWebAuthnRepository` call
in `deps.go`, the `applyWebAuthnConfig` call in `internal/core/auth/config.go`,
`internal/modules/auth/webauthn.go` and the WebAuthn block in `routes.go`.
Leaving them in place while disabled costs nothing at runtime.
