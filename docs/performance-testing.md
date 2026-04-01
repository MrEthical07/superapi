# Performance Testing Runbook

This runbook defines the strict auth-correct validation flow for 10K RPS.

## Validity rules

- 401 responses are failures.
- Shared global bearer token is not allowed.
- Each VU must login independently and refresh before expiry.
- A run is invalid when any of the following is true:
  - total error rate > 1%
  - dropped iterations > 0
  - sustain phase throughput < 95% of target
  - route-level 401 rate > 1%

## 1) Environment preflight

Use full stack in production mode. Use strict auth mode so whoami validation stays consistent.

```bash
APP_ENV=production
LOG_LEVEL=warn
LOG_FORMAT=json
HTTP_ADDR=:8080

POSTGRES_ENABLED=true
POSTGRES_URL=postgres://user:pass@host:5432/db?sslmode=disable
POSTGRES_MAX_CONNS=80
POSTGRES_MIN_CONNS=20

REDIS_ENABLED=true
REDIS_ADDR=127.0.0.1:6379
REDIS_POOL_SIZE=80
REDIS_MIN_IDLE_CONNS=20

AUTH_ENABLED=true
AUTH_MODE=strict

RATELIMIT_ENABLED=true
RATELIMIT_FAIL_OPEN=false
RATELIMIT_DEFAULT_LIMIT=500000
RATELIMIT_DEFAULT_WINDOW=1m

CACHE_ENABLED=true
CACHE_FAIL_OPEN=false

METRICS_AUTH_TOKEN=perf-metrics-token
HTTP_MIDDLEWARE_ACCESS_LOG_SAMPLE_RATE=0
```

Validate readiness before load:

```bash
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/readyz
```

## 2) Auth endpoints used by k6

- POST /api/v1/system/auth/login
- POST /api/v1/system/auth/refresh
- GET /api/v1/system/whoami

Login/refresh responses return:

- access_token
- refresh_token
- access_expires_unix

## 3) Seed per-VU user pool

Seed enough users for the expected VU range before load. This avoids login contention against a single account.

```powershell
powershell -ExecutionPolicy Bypass -File performance/k6/seed-users.ps1 -Count 500 -Prefix loadtest -Domain example.com -Password LoadTest123! -AuthMode strict
```

## 4) Staged validation before full run

### Step 1: single request check

```powershell
$loginBody = @{ identifier = "loadtest+vu1@example.com"; password = "LoadTest123!" } | ConvertTo-Json
$login = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:8080/api/v1/system/auth/login" -Body $loginBody -ContentType "application/json"
$token = $login.data.access_token
Invoke-RestMethod -Method Get -Uri "http://127.0.0.1:8080/api/v1/system/whoami" -Headers @{ Authorization = "Bearer $token" }
```

Expected: 200 for both calls.

### Step 2: 100 RPS burst

```bash
BASE_URL=http://127.0.0.1:8080 \
AUTH_IDENTIFIER=loadtest@example.com \
AUTH_PASSWORD=LoadTest123! \
AUTH_IDENTIFIER_PREFIX=loadtest \
AUTH_IDENTIFIER_DOMAIN=example.com \
AUTH_USER_POOL_SIZE=500 \
START_RPS=100 \
TARGET_RPS=100 \
RAMP_DURATION=30s \
SUSTAIN_DURATION=1m \
RAMP_STEPS=1 \
P50_THRESHOLD_MS=50 \
P95_THRESHOLD_MS=100 \
P99_THRESHOLD_MS=250 \
ERROR_THRESHOLD=0.01 \
k6 run --summary-export performance/results/k6-100-summary.json performance/k6/scenario.js
```

Expected: no 401 spikes, no dropped iterations.

### Step 3: 1K short run

```bash
BASE_URL=http://127.0.0.1:8080 \
AUTH_IDENTIFIER=loadtest@example.com \
AUTH_PASSWORD=LoadTest123! \
AUTH_IDENTIFIER_PREFIX=loadtest \
AUTH_IDENTIFIER_DOMAIN=example.com \
AUTH_USER_POOL_SIZE=500 \
START_RPS=200 \
TARGET_RPS=1000 \
RAMP_DURATION=1m \
SUSTAIN_DURATION=2m \
RAMP_STEPS=6 \
P50_THRESHOLD_MS=50 \
P95_THRESHOLD_MS=100 \
P99_THRESHOLD_MS=250 \
ERROR_THRESHOLD=0.01 \
k6 run --summary-export performance/results/k6-1k-summary.json performance/k6/scenario.js
```

Expected: stable latency, no significant 401 rate, no dropped iterations.

## 5) Full validated run at 10K

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

## 6) Required artifacts

- performance/results/k6-summary.json
- performance/results/error-breakdown.json
- performance/results/latency-histogram.json
- performance/results/rps-over-time.csv
- performance/results/vu-usage-over-time.csv

Generate artifact files from k6 summary:

```powershell
powershell -ExecutionPolicy Bypass -File performance/k6/export-artifacts.ps1 -SummaryPath performance/results/k6-summary.json -OutputDir performance/results
```

## 7) Pass/fail decision

Pass only when all conditions hold:

1. Sustained throughput reaches at least 95% of 10K target.
2. Total request failures stay at or below 1%.
3. Route-level 401 rate is at or below 1%.
4. Dropped iterations are exactly zero.
5. Successful-request p95 and p99 are within SLO thresholds.
