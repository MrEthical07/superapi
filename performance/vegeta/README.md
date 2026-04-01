# Vegeta Readiness Mix (10K RPS)

This runner mirrors the k6 representative traffic profile:

- `GET /healthz` (35%)
- `GET /readyz` (25%)
- `POST /system/parse-duration` (20%)
- `GET /api/v1/system/whoami` with bearer token (20%)

The script performs:

1. Ramp: integer-minute step ramp to target RPS (default `10m`).
2. Sustain: fixed target RPS soak (default `30m`).
3. Reports: text/json/histogram + HTML plot.

## Prerequisites

- API running in full mode.
- Valid auth token for `whoami` traffic.
- `vegeta` installed and in `PATH`.
- PowerShell 5.1+ (Windows).

## Run

```powershell
powershell -ExecutionPolicy Bypass -File performance/vegeta/run.ps1 \
  -BaseUrl "http://127.0.0.1:8080" \
  -AuthToken "<token>" \
  -Rate 10000 \
  -RampDuration "10m" \
  -SustainDuration "30m" \
  -Treat401AsExpected $true \
  -OutputDir "performance/results/vegeta"
```

`-Treat401AsExpected` (default `true`) reports an adjusted success ratio that treats 401 as expected for expiring auth tokens on `whoami` traffic.

## Outputs

- `performance/results/vegeta/targets.txt`
- `performance/results/vegeta/ramp-step-*.bin`
- `performance/results/vegeta/sustain.bin`
- `performance/results/vegeta/report.txt`
- `performance/results/vegeta/report.json`
- `performance/results/vegeta/histogram.txt`
- `performance/results/vegeta/plot.html`
