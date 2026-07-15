# Enabling WebAuthn

WebAuthn is fully scaffolded but **disabled by default**. Nothing about it is
active until you enable it, and it forces no schema on deployments that never
turn it on. This page covers what ships, and the two steps to turn it on.

## What ships (disabled)

- goAuth's WebAuthn config surface, wired from `WEBAUTHN_*` env in
  `internal/core/auth/config.go` (`WEBAUTHN_ENABLED=false` by default).
- `StoreUserProvider` implements `goauth.WebAuthnCredentialProvider`, backed by
  a WebAuthn credential repository over the sqlc boundary
  (`internal/core/auth/webauthn_repository.go`).
- Ceremony endpoints under `/api/v1/system/auth/webauthn/*` (register begin /
  finish, list credentials, remove credential), auth-protected. While WebAuthn
  is disabled these return a "webauthn disabled" error (403-class), so they act
  as a working, self-documenting example.
- An **optional** migration, `db/migrations/000004_webauthn_credentials.up.sql`,
  and its sqlc schema mirror + queries.

Because goAuth only requires the WebAuthn credential capability when
`WebAuthn.Enabled` is true, shipping the provider methods and endpoints while
disabled is safe — `Build()` does not fail.

## Step 1 — apply the optional migration

The `webauthn_credentials` table only needs to exist once WebAuthn is enabled:

```
make migrate-up DB_URL="postgres://..."
```

(or apply `db/migrations/000004_webauthn_credentials.up.sql` with your migration
tool of choice). To revert, use the paired `...down.sql`.

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

1. `POST /api/v1/system/auth/webauthn/register/begin` → returns `ceremony_id`
   and `options_json`. Pass `options_json` to `navigator.credentials.create`.
2. `POST /api/v1/system/auth/webauthn/register/finish` with the `ceremony_id`
   and the authenticator's `response_json` → persists the credential.
3. `GET /api/v1/system/auth/webauthn/credentials` lists a user's credentials;
   `POST .../credentials/remove` removes one by base64url credential id.

Login assertions are completed through the MFA confirm endpoint with
`type: "webauthn"` once `WEBAUTHN_REQUIRE_FOR_LOGIN` (or per-user credentials)
brings WebAuthn into the login path. goAuth manages the single-use ceremony
state in Redis under the `awn:` prefix.

## Removing WebAuthn entirely

If you will never use WebAuthn, you can delete the optional migration, schema
mirror, and queries (`*webauthn_credentials*`), the WebAuthn repository, the
provider methods, and the ceremony routes. Leaving them in place while disabled
costs nothing at runtime.
