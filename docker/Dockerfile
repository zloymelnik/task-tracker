# syntax=docker/dockerfile:1

# -------------------------------------------------------------
# Base: current version of Go on Alpine
# -------------------------------------------------------------
FROM golang:1.26.10-alpine AS base

WORKDIR /app

RUN apk add --no-cache \
    git \
    make \
    curl \
    bash \
    ca-certificates \
    tzdata

# -------------------------------------------------------------
# Local: development tools for devcontainers
# -------------------------------------------------------------
FROM base AS local

RUN apk add --no-cache gcc musl-dev

ENV CGO_ENABLED=1

COPY --from=golangci/golangci-lint:v2.10.1-alpine /usr/bin/golangci-lint /usr/local/bin/golangci-lint

RUN go install github.com/air-verse/air@v1.67.4 && \
    go install -tags 'mysql' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.2 && \
    go install github.com/swaggo/swag/cmd/swag@v1.16.4

ARG UID=1001
ARG GID=1001
RUN addgroup -g ${GID} appgroup && \
    adduser -u ${UID} -G appgroup -D appuser && \
    mkdir -p /go/pkg /app && \
    chown -R appuser:appgroup /go /app

USER appuser

EXPOSE 8080

CMD ["tail", "-f", "/dev/null"]

# -------------------------------------------------------------
# Builder: building the binary
# -------------------------------------------------------------
FROM base AS builder

COPY go.* ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /bin/api ./cmd/task-tracker

# -------------------------------------------------------------
# Prod: minimal runtime
# -------------------------------------------------------------
FROM alpine:3.22 AS prod

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

COPY --from=builder /bin/api /app/api
COPY --from=builder /app/configs/config.yaml /app/configs/config.yaml

USER appuser

EXPOSE 8080

ENTRYPOINT ["/app/api"]
