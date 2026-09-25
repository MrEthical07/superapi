# Performance Assets

Load-testing and benchmarking assets. Optional: `make init flags=--no-perf`
removes this folder, `cmd/perftoken` and the `make load-*` targets.

The tooling is **Windows/PowerShell-first** (it was built on Windows). POSIX
ports exist for the Vegeta runner and user seeding; `export-artifacts.ps1` is
PowerShell-only.

| Task | Windows (PowerShell) | Linux/macOS |
|---|---|---|
| Vegeta ramp + reports | `vegeta/run.ps1` | `vegeta/run.sh` (`make load-vegeta-10k` picks automatically) |
| Seed k6 users | `k6/seed-users.ps1` | `k6/seed-users.sh` |
| k6 scenario | `make load-k6-10k` | `make load-k6-10k` |
| Export k6 artifacts | `k6/export-artifacts.ps1` | not ported |

## Structure

- `performance/k6/` — k6 scenario for 10K RPS readiness profile.
- `performance/vegeta/` — Vegeta runner and report generation.
- `performance/results/` — runtime outputs (gitignored).

`AUTH_TEST_*` overrides used by these runs are refused unless `APP_ENV` is
`dev` or `test`. Use `docs/performance-testing.md` as the runbook.
