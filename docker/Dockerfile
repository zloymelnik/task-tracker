# syntax=docker/dockerfile:1

# -------------------------------------------------------------
# Base: актуальная версия Go на Alpine
# -------------------------------------------------------------
FROM golang:1.26-alpine AS base

WORKDIR /app

RUN apk add --no-cache \
    git \
    make \
    curl \
    bash \
    ca-certificates \
    tzdata

# -------------------------------------------------------------
# Local: инструменты разработки под devcontainer
# -------------------------------------------------------------
FROM base AS local

# Установка актуальных стабильных утилит
RUN go install github.com/air-verse/air@v1.67.4 && \
    go install -tags 'mysql' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.2 && \
    go install github.com/swaggo/swag/cmd/swag@v1.16.4 && \
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.5

# Непривилегированный пользователь
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
# Builder: сборка бинарника
# -------------------------------------------------------------
FROM base AS builder

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /bin/api cmd/api/main.go

# -------------------------------------------------------------
# Prod: минимальный runtime
# -------------------------------------------------------------
FROM alpine:3.22 AS prod

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

COPY --from=builder /bin/api /app/api

USER appuser

EXPOSE 8080

ENTRYPOINT ["/app/api"]