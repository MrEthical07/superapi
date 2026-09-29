// Command rotatetotpkey re-encrypts every stored TOTP secret under the active
// encryption key, so an old key can be retired without waiting for users to log
// in (a login re-encrypts that user's secret lazily).
//
// Usage:
//
//	make rotate-totp-key
//	make rotate-totp-key dry_run=1
//	go run ./cmd/rotatetotpkey [--batch-size 200] [--dry-run]
//
// It reads the same environment as the API (AUTH_TOTP_ENCRYPTION_KEY and/or
// AUTH_TOTP_ENCRYPTION_KEYS + AUTH_TOTP_ENCRYPTION_ACTIVE_KID, POSTGRES_URL) and
// needs every key that sealed a row to still be configured. Rows sealed with a
// missing key are reported and left unchanged. Progress is printed per batch;
// the exit status is non-zero if any secret could not be re-encrypted. See
// docs/security-env-recommendations.md for the rotation procedure.
package main
