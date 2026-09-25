#!/usr/bin/env bash
# POSIX port of seed-users.ps1: creates COUNT load-test users via cmd/perftoken.
#
#   COUNT=500 PREFIX=loadtest DOMAIN=example.com performance/k6/seed-users.sh
#
# Uses POSTGRES_URL / REDIS_ADDR from the environment (or .env via make).
set -euo pipefail

COUNT="${COUNT:-500}"
PREFIX="${PREFIX:-loadtest}"
DOMAIN="${DOMAIN:-example.com}"
PASSWORD="${PASSWORD:-LoadTest123!}"
ROLE="${ROLE:-user}"
AUTH_MODE="${AUTH_MODE:-strict}"

[ "$COUNT" -gt 0 ] || { echo "COUNT must be > 0" >&2; exit 2; }
export POSTGRES_ENABLED=true REDIS_ENABLED=true AUTH_MODE
export POSTGRES_URL="${POSTGRES_URL:-postgres://superapi:superapi@127.0.0.1:5432/superapi?sslmode=disable}"
export REDIS_ADDR="${REDIS_ADDR:-127.0.0.1:6379}"

bin_dir="$(mktemp -d)"
trap 'rm -rf "$bin_dir"' EXIT
go build -o "$bin_dir/perftoken" ./cmd/perftoken
for ((i = 1; i <= COUNT; i++)); do
  email="${PREFIX}+vu${i}@${DOMAIN}"
  "$bin_dir/perftoken" --email "$email" --password "$PASSWORD" --role "$ROLE" --mode "$AUTH_MODE" --create-if-missing=true --output json >/dev/null
  if (( i % 50 == 0 || i == COUNT )); then echo "SEEDED_USERS=$i"; fi
done
echo "SEED_COMPLETE total=$COUNT"
