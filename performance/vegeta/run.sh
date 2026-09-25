#!/usr/bin/env bash
# POSIX port of run.ps1: ramped Vegeta attack + reports.
#
#   performance/vegeta/run.sh -t "$ACCESS_TOKEN" [-b http://127.0.0.1:8080] \
#     [-r 10000] [-u 10m] [-s 30m] [-o performance/results/vegeta]
#
# Requires vegeta; jq is optional (used for the summary line).
set -euo pipefail

BASE_URL="http://127.0.0.1:8080"
TOKEN=""
RATE=10000
RAMP="10m"
SUSTAIN="30m"
OUT="performance/results/vegeta"
VEGETA="${VEGETA:-vegeta}"

while getopts "b:t:r:u:s:o:" opt; do
  case "$opt" in
    b) BASE_URL="$OPTARG" ;;
    t) TOKEN="$OPTARG" ;;
    r) RATE="$OPTARG" ;;
    u) RAMP="$OPTARG" ;;
    s) SUSTAIN="$OPTARG" ;;
    o) OUT="$OPTARG" ;;
    *) echo "usage: $0 -t token [-b base_url] [-r rate] [-u ramp] [-s sustain] [-o out_dir]" >&2; exit 2 ;;
  esac
done

command -v "$VEGETA" >/dev/null || { echo "required command not found: $VEGETA" >&2; exit 1; }
[ -n "$TOKEN" ] || { echo "-t token is required for authenticated /api/v1/auth/whoami traffic" >&2; exit 2; }
[ "$RATE" -gt 0 ] || { echo "rate must be > 0" >&2; exit 2; }
[[ "$RAMP" =~ ^([0-9]+)m$ ]] || { echo "ramp must be whole minutes, e.g. 10m" >&2; exit 2; }
STEPS="${BASH_REMATCH[1]}"
BASE_URL="${BASE_URL%/}"

mkdir -p "$OUT"
TARGETS="$OUT/targets.txt"
: > "$TARGETS"
emit() { # weight method path [header]
  local i
  for ((i = 0; i < $1; i++)); do
    printf '%s %s%s\n' "$2" "$BASE_URL" "$3" >> "$TARGETS"
    [ -n "${4:-}" ] && printf '%s\n' "$4" >> "$TARGETS"
    printf '\n' >> "$TARGETS"
  done
}
emit 35 GET /healthz
emit 25 GET /readyz
emit 40 GET /api/v1/auth/whoami "Authorization: Bearer $TOKEN"

RESULTS=()
for ((step = 1; step <= STEPS; step++)); do
  step_rate=$(( (RATE * step + STEPS / 2) / STEPS ))
  (( step_rate < 1 )) && step_rate=1
  file=$(printf '%s/ramp-step-%02d.bin' "$OUT" "$step")
  echo "[vegeta] Ramp step $step/$STEPS @ $step_rate rps for 1m"
  "$VEGETA" attack -targets="$TARGETS" -rate="$step_rate/1s" -duration=1m -output="$file"
  RESULTS+=("$file")
done

echo "[vegeta] Sustain @ $RATE rps for $SUSTAIN"
"$VEGETA" attack -targets="$TARGETS" -rate="$RATE/1s" -duration="$SUSTAIN" -output="$OUT/sustain.bin"
RESULTS+=("$OUT/sustain.bin")

echo "[vegeta] Writing reports"
"$VEGETA" report -type=text "${RESULTS[@]}" | tee "$OUT/report.txt"
"$VEGETA" report -type=json "${RESULTS[@]}" > "$OUT/report.json"
"$VEGETA" report -type='hist[0,50ms,100ms,250ms,500ms,1s,2s]' "${RESULTS[@]}" > "$OUT/histogram.txt"
"$VEGETA" plot "${RESULTS[@]}" > "$OUT/plot.html"

if command -v jq >/dev/null; then
  jq -r '"[vegeta] Summary: total=\(.requests) status200=\(.status_codes["200"] // 0) status401=\(.status_codes["401"] // 0) status0=\(.status_codes["0"] // 0)"' "$OUT/report.json"
fi
echo "[vegeta] Completed. Results in $OUT"
