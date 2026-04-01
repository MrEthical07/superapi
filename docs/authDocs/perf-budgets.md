# Performance Budgets and Regression Gate

This document defines lightweight, enforceable performance guardrails for core
auth flows.

## Scope

Tracked benchmarks (from `auth_bench_test.go`):

- `BenchmarkValidateJWTOnly`
- `BenchmarkValidateStrict`
- `BenchmarkRefresh`

Baseline samples are stored in:

- A versioned baseline artifact in your repository (for example `performance/results/auth_bench_baseline.txt`)

## Budget Targets

These are the current working budgets for regression detection:

| Benchmark | Primary budget | Secondary budget | Notes |
| --- | --- | --- | --- |
| `BenchmarkValidateJWTOnly` | `ns/op` should stay near baseline; fail on > +30% | `allocs/op` should remain allocation-stable; fail on > +30% | Hot path budget check. |
| `BenchmarkValidateStrict` | `ns/op` should stay near baseline; fail on > +30% | `allocs/op` should remain allocation-stable; fail on > +30% | Includes Redis-backed strict checks. |
| `BenchmarkRefresh` | `ns/op` should stay near baseline; fail on > +30% | Throughput implied by `ns/op`; monitor `ops/sec` trend in load test reports | Rotation path sanity gate. |

`BenchmarkRefresh` throughput interpretation:

- approx `ops/sec = 1e9 / ns_per_op`

## CI Gate

CI runs:

```bash
go test -run '^$' -bench 'Benchmark(Validate|Refresh)' -benchmem -count=5 ./...
```

Recommended gate workflow:

1. Executes the benchmark subset with `-count=5`.
2. Compares baseline vs candidate with `benchstat`.
3. Enforces a +30% regression threshold in CI.

The gate is intentionally tolerant of small jitter but blocks large slow-path
regressions.

## Updating Baseline

When an intentional perf change is accepted:

1. Re-run the benchmark subset on representative hardware.
2. Update your repository baseline artifact (for example `performance/results/auth_bench_baseline.txt`).
3. Keep the rationale in the PR description (what changed and why).
