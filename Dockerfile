# syntax=docker/dockerfile:1
#
# Production image: static binaries on distroless, running as non-root.
#
#   docker build -t superapi .
#   docker run --rm -p 8080:8080 --env-file .env superapi
#   docker run --rm --env-file .env superapi /app/migrate up   # run migrations
#
# The runtime config is read from the environment only (no .env inside).

FROM golang:1.26.5-bookworm AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w" -o /out/api ./cmd/api && \
    go build -ldflags="-s -w" -o /out/migrate ./cmd/migrate && \
    go build -ldflags="-s -w" -o /out/createuser ./cmd/createuser

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/api /out/migrate /out/createuser /app/
COPY db/migrations /app/db/migrations
USER nonroot:nonroot
EXPOSE 8080
ENV HTTP_ADDR=:8080
ENTRYPOINT []
CMD ["/app/api"]
