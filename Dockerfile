# syntax=docker/dockerfile:1

# ---- Build stage ----
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

# Build the static binary (modernc.org/sqlite is pure Go, no CGO needed).
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/swallow-monitor .

# ---- Runtime stage ----
FROM alpine:3.20

# ca-certificates for outbound HTTPS to the GitHub OAuth API.
RUN apk add --no-cache ca-certificates && \
    adduser -D -u 10001 swallow

WORKDIR /app

COPY --from=builder /out/swallow-monitor /app/swallow-monitor

# Data directory for the SQLite database.
RUN mkdir -p /data && chown -R swallow:swallow /data /app
USER swallow

EXPOSE 8080
VOLUME ["/data"]

ENTRYPOINT ["/app/swallow-monitor"]
CMD ["-c", "/app/config.yaml"]
