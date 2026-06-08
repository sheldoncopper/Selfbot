# syntax=docker/dockerfile:1

# ---- build stage ----------------------------------------------------------
FROM golang:1.26-alpine AS build
WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

# Build a fully static, pure-Go binary (no CGO — modernc SQLite).
COPY . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/selfbot ./cmd/selfbot

# ---- runtime stage --------------------------------------------------------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/selfbot /app/selfbot

# Default DB path inside the container; mount a volume here on Railway.
ENV DB_PATH=/data/selfbot.sqlite
RUN mkdir -p /data && chown app:app /data
USER app
VOLUME ["/data"]

ENTRYPOINT ["/app/selfbot"]
