// Command createuser creates an account through the same goAuth engine the API
// server builds from config (bootstrap admin, first user, ops tooling).
//
// Usage:
//
//	make user email=admin@example.com role=admin
//	go run ./cmd/createuser --email admin@example.com --role admin
//	printf '%s\n' "$PASSWORD" | go run ./cmd/createuser --email a@example.com --password-stdin
//
// The password is never accepted as a flag (it would land in shell history
// and process listings): it is prompted for without echo on a terminal, or
// read from stdin with --password-stdin.
package main
