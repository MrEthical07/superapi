package auth

import "strings"

// NormalizeIdentifier returns the canonical form of a login identifier: trimmed
// and lower-cased. Identifiers are emails, so "Alice@Example.com" and
// "alice@example.com" must be the same account, and must share goAuth's
// per-identifier login and reset limiters rather than each getting their own.
//
// Every place that hands an identifier to goAuth, or stores or looks one up,
// uses this one helper: the auth module (register, login, password-reset and
// email-verification requests), the user repository, cmd/createuser and
// cmd/perftoken. The SQL compares lower(email), so a row stored by an older
// release with mixed case still matches.
func NormalizeIdentifier(identifier string) string {
	return strings.ToLower(strings.TrimSpace(identifier))
}
