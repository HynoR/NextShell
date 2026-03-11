# ---- build stage ----
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Cache dependencies separately from source
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0: modernc.org/sqlite is pure-Go, no cgo needed
RUN CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags "-s -w" \
      -o nshellserver \
      .

# ---- runtime stage ----
FROM alpine:3

# ca-certificates is needed for outbound TLS (e.g. future webhook calls)
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/nshellserver .

# /data  → SQLite database (mount a named volume or bind-mount)
# /certs → TLS cert + key (mount read-only)
VOLUME ["/data", "/certs"]

EXPOSE 8443

ENTRYPOINT ["/app/nshellserver"]
