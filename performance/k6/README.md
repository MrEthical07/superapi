# k6 Auth-Correct Readiness Mix

This scenario enforces strict auth-correct behavior under open-model load.

## Traffic mix

- GET /healthz (35%)
- GET /readyz (25%)
- POST /system/parse-duration (20%)
- GET /api/v1/system/whoami (20%)

All workload requests carry a bearer token obtained by each VU from login and refreshed before expiry.

## Recommended auth setup

- Run API with AUTH_MODE=strict.
- Seed a user pool before test execution:

```powershell
powershell -ExecutionPolicy Bypass -File performance/k6/seed-users.ps1 -Count 500 -Prefix loadtest -Domain example.com -Password LoadTest123! -AuthMode strict
```

## Auth lifecycle

1. Each VU logs in through POST /api/v1/system/auth/login.
2. Each VU stores access token, refresh token, and access expiry.
3. Each VU refreshes with POST /api/v1/system/auth/refresh before expiry buffer.
4. On any 401, the script refreshes once and retries once.
5. 401 responses are still counted as failures.

## Run

```bash
BASE_URL=http://127.0.0.1:8080 \
AUTH_IDENTIFIER=loadtest@example.com \
AUTH_PASSWORD=LoadTest123! \
AUTH_IDENTIFIER_PREFIX=loadtest \
AUTH_IDENTIFIER_DOMAIN=example.com \
AUTH_USER_POOL_SIZE=500 \
START_RPS=500 \
TARGET_RPS=10000 \
RAMP_DURATION=5m \
SUSTAIN_DURATION=10m \
RAMP_STEPS=10 \
PREALLOCATED_VUS=2000 \
MAX_VUS=30000 \
REFRESH_BUFFER_SECONDS=30 \
SUSTAIN_RPS_RATIO=0.95 \
P50_THRESHOLD_MS=50 \
P95_THRESHOLD_MS=100 \
P99_THRESHOLD_MS=250 \
ERROR_THRESHOLD=0.01 \
k6 run --summary-export performance/results/k6-summary.json performance/k6/scenario.js
```

## Thresholds enforced

- http_req_failed rate < ERROR_THRESHOLD
- dropped_iterations count == 0
- successful_req_duration p50/p95/p99 under configured limits
- sustain scenario throughput >= TARGET_RPS * SUSTAIN_RPS_RATIO
- status_401_rate <= AUTH_401_RATE_THRESHOLD
- auth lifecycle failures == 0

## Useful environment variables

- AUTH_LOGIN_PATH (default /api/v1/system/auth/login)
- AUTH_REFRESH_PATH (default /api/v1/system/auth/refresh)
- REQUEST_TIMEOUT (default 5s)
- AUTH_401_RATE_THRESHOLD (default 0.01)
- AUTH_IDENTIFIER_PREFIX and AUTH_USER_POOL_SIZE for per-VU identity assignment

## Artifacts

Run with summary export and then generate additional artifacts:

```powershell
powershell -ExecutionPolicy Bypass -File performance/k6/export-artifacts.ps1 -SummaryPath performance/results/k6-summary.json -OutputDir performance/results
```
