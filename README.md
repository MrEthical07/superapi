# api-template

A security-first, high-performance Go API template for SaaS projects.

## Requirements
- Go 1.26+

## Quick start
```bash
go test ./...
go run .
```

## HTTP middleware config

Global (server-level) middleware is configured via environment variables:

- `HTTP_MIDDLEWARE_REQUEST_ID_ENABLED` (default: `true`)
- `HTTP_MIDDLEWARE_RECOVERER_ENABLED` (default: `true`)
- `HTTP_MIDDLEWARE_MAX_BODY_BYTES` (default: `0`, disabled)
- `HTTP_MIDDLEWARE_SECURITY_HEADERS_ENABLED` (default: `false`)
- `HTTP_MIDDLEWARE_REQUEST_TIMEOUT` (default: `0`, disabled)

Notes:
- `HTTP_MIDDLEWARE_MAX_BODY_BYTES` must be `>= 0`.
- `HTTP_MIDDLEWARE_REQUEST_TIMEOUT` must be a valid duration and `>= 0`.