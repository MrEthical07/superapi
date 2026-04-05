# Contributing

This repository is published as a template for production-grade Go APIs.

## Before You Start
- Do not install this repository via `go get`.
- Use GitHub "Use this template" to create your own project.
- Template-generated projects are snapshots and do not auto-update from this source.

## Local Development
- Run API: `go run ./cmd/api`
- Format code: `make fmt`
- Vet code: `make vet`
- Run tests: `go test ./...`
- Build all packages: `go build ./...`

## Adding a Module
- Generate baseline module: `make module name=projects`
- Optional DB scaffolding: `make module name=projects db=1`
- Optional policy examples: `make module name=projects auth=1 tenant=1 ratelimit=1 cache=1`
- Read module docs: `docs/modules.md` and `docs/crud-examples.md`

## Testing Expectations
- Run `go test ./...` before opening a PR.
- Run `go build ./...` before opening a PR.
- For hot-path changes, run `make bench-hotpath` and include before/after results.

## Coding Standards
- Follow repository engineering rules in `AGENTS.md`.
- Keep handlers/services explicit and small.
- Prefer explicit interfaces over hidden magic.
- Keep hot paths lean and production-safe.
- Reuse typed app errors and centralized response handling.

## Governance Rules
- No breaking changes without a version bump and migration notes.
- Core changes require a pull request review.
- Documentation updates are required for behavior/config/API changes.
- Core modifications must be contributed back via pull requests.

## Pull Request Checklist
- Tests pass: `go test ./...`
- Build passes: `go build ./...`
- Relevant docs updated
- Changelog updated when release-impacting
- Backward compatibility considered or clearly documented

## Release Preparation Checklist
- Update `CHANGELOG.md` with a new top release section (breaking changes, added, changed, removed, docs).
- Update release metadata in `README.md`:
	- release badge version
	- current public template baseline version
- Run quality gates:
	- `go test ./...`
	- `go build ./...`
	- `make verify`
- Confirm docs that describe architecture/runtime behavior are aligned with current code paths.
- Create a release commit.
- Create annotated release tag after merge/publish prep:
	- `git tag -a vX.Y.Z -m "Release vX.Y.Z"`
	- `git push origin vX.Y.Z`
