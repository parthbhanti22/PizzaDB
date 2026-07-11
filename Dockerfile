# ============================================================
# PizzaDB — Production Multi-Stage Dockerfile
# Target: linux/arm64 (OCI Ampere A1 Always-Free instances)
# ============================================================

# ── Stage 1: Builder ────────────────────────────────────────
FROM golang:1.23-alpine AS builder

# Install git + ca-certs (needed if dependencies use HTTPS)
RUN apk add --no-cache git ca-certificates

WORKDIR /build

# Copy dependency manifests first for Docker layer caching.
# If go.mod/go.sum haven't changed, this layer is cached and
# 'go mod download' is skipped on subsequent builds.
COPY go.mod ./
RUN go mod download

# Copy the full source tree
COPY . .

# Compile a fully static binary.
# CGO_ENABLED=0  → pure Go, no C dependencies (critical for alpine/scratch)
# -ldflags="-s -w" → strip debug symbols + DWARF info (~30% smaller binary)
# -o /pizzadb → output path for the final binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /pizzadb .

# ── Stage 2: Runner ────────────────────────────────────────
FROM alpine:latest

# Add ca-certificates for any future HTTPS needs and
# tzdata so Go's time package works correctly
RUN apk add --no-cache ca-certificates tzdata

# Create a non-root user for security
RUN addgroup -S pizzadb && adduser -S pizzadb -G pizzadb

# Create a data directory for the .db files (mountable as a volume)
RUN mkdir -p /data && chown pizzadb:pizzadb /data

WORKDIR /data

# Copy the statically-linked binary from the builder stage
COPY --from=builder /pizzadb /usr/local/bin/pizzadb

# Raft RPC (inter-node consensus)
EXPOSE 8001
# PizzaQL TCP Gateway (client-facing data plane)
EXPOSE 13001

# Switch to non-root user
USER pizzadb

# ENTRYPOINT runs the binary; CMD provides default flags.
# Override CMD at runtime with: docker run ... pizzadb -id <addr> -peers <peers>
ENTRYPOINT ["pizzadb"]
CMD ["-id", "0.0.0.0:8001", "-peers", "node2:8001,node3:8001"]
