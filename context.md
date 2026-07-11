# PizzaDB — AI State Handoff Document

> **Last Updated:** 2026-06-12
> **Status:** Data Plane v1 complete — TCP Gateway, Auth, PizzaQL Parser, DEL support
> **Module:** `github.com/parthbhanti22/PizzaDB`
> **Go Version:** 1.23.1

---

## Architecture Overview

```
┌──────────────────────────────────────────────────────────────────┐
│                        PizzaDB Node                              │
│                                                                  │
│  ┌─────────────────┐   ┌──────────────────┐   ┌──────────────┐  │
│  │  Raft RPC        │   │  Binary Server    │   │  PizzaQL     │  │
│  │  (inter-node)    │   │  (legacy client)  │   │  TCP Gateway │  │
│  │  Port: 8001      │   │  Port: 9001       │   │  Port: 13001 │  │
│  │  server.go        │   │  server.go        │   │  server/     │  │
│  └────────┬─────────┘   └────────┬──────────┘   └───────┬──────┘  │
│           │                      │                      │         │
│           │              ┌───────┴──────────────────────┤         │
│           │              │                              │         │
│           ▼              ▼                              ▼         │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │                     RaftNode (raft.go)                       │  │
│  │  • Leader Election  • Log Replication  • Propose()          │  │
│  └─────────────────────────┬───────────────────────────────────┘  │
│                            │ applyCh (committed commands)         │
│                            ▼                                      │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │                     DB (db.go)                               │  │
│  │  • Bitcask-style append-only log                             │  │
│  │  • In-memory keyDir index                                    │  │
│  │  • Set() / Get() / Delete() (tombstone)                      │  │
│  │  • Crash recovery via recover()                              │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                                                                   │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │                     Encoder (encoder.go)                     │  │
│  │  • 16-byte header: [KeyLen:4][ValLen:4][Timestamp:8]         │  │
│  │  • Encode(key, value) → []byte                               │  │
│  │  • Decode([]byte) → Entry{Key, Value, Timestamp}             │  │
│  └─────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

---

## Port Allocation (Per Node)

| Port Formula     | Default (Node 1) | Purpose                        |
| ---------------- | ----------------- | ------------------------------ |
| `{raftPort}`     | `8001`            | Raft RPC (inter-node consensus)|
| `{raftPort}+1000`| `9001`            | Legacy binary-protocol server  |
| `{raftPort}+5000`| `13001`           | PizzaQL TCP Gateway (new)      |

For a 3-node local cluster:
- Node 1: Raft=8001, Binary=9001, Gateway=13001
- Node 2: Raft=8002, Binary=9002, Gateway=13002
- Node 3: Raft=8003, Binary=9003, Gateway=13003

---

## File Map

| File                  | Package  | Purpose                                              |
| --------------------- | -------- | ---------------------------------------------------- |
| `main.go`             | `main`   | Entry point: flag parsing, wiring, goroutine launch  |
| `raft.go`             | `main`   | Full Raft implementation (election, heartbeat, replication, Propose) |
| `db.go`               | `main`   | Bitcask storage engine (Set, Get, Delete/tombstone, recover) |
| `encoder.go`          | `main`   | Binary encoder/decoder for disk log format           |
| `server.go`           | `main`   | Legacy binary-protocol TCP server (0x01=SET, 0x02=GET) |
| `parser/pizzaql.go`   | `parser` | PizzaQL text tokenizer with double-quote support     |
| `server/auth.go`      | `server` | Thread-safe AuthManager (RWMutex token map)          |
| `server/tcp.go`       | `server` | PizzaQL TCP Gateway (line-based, auth-gated)         |
| `client/main.go`      | `main`   | Test client for binary protocol                      |
| `chaos/main.go`       | `main`   | Stress test (chaos monkey) for binary protocol       |
| `db_test.go`          | `main`   | Benchmarks (Set/Get/Delete/Recovery) + correctness tests |

---

## PizzaQL Protocol Specification

### Transport
- **Protocol:** TCP, newline-delimited (`\n` or `\r\n`)
- **Encoding:** UTF-8 plain text
- **Max line size:** 1 MB (configurable in scanner buffer)

### Authentication Flow
Every new TCP connection MUST authenticate before issuing data commands:

```
Client → Server:  AUTH pizzadb-default-token-2026\n
Server → Client:  +OK Authenticated\r\n
```

Until authenticated, only `PING` and `AUTH` are accepted. All other commands return:
```
-ERR not authenticated. Send AUTH <token> first\r\n
```

### Commands

| Command | Syntax | Description | Response |
|---------|--------|-------------|----------|
| `AUTH`  | `AUTH <token>` | Authenticate the connection | `+OK Authenticated` or `-ERR invalid token` |
| `PING`  | `PING` | Health check (works pre-auth) | `+PONG` |
| `SET`   | `SET <key> <value>` | Write key-value (goes through Raft) | `+OK` or `-ERR not leader` |
| `GET`   | `GET <key>` | Read from local storage | `+OK <value>` or `-ERR key not found: <key>` |
| `DEL`   | `DEL <key>` | Delete key (tombstone, goes through Raft) | `+OK` or `-ERR not leader` |

### Quoted String Values
Values containing spaces or JSON can be wrapped in double quotes:
```
SET user:1001 "{\"name\": \"Parth Bhanti\", \"role\": \"Chef\"}"
```
The parser strips the outer quotes and preserves internal content including escaped quotes.

### Response Format
- **Success:** `+OK` or `+OK <data>\r\n`
- **Error:** `-ERR <message>\r\n`
- **Ping:** `+PONG\r\n`

---

## Data Flow

### SET (Write Path)
```
Client  ──SET key val──►  TCPGateway.handleConn()
                              │
                              ▼
                         parser.Parse("SET key val")
                              │
                              ▼
                         raft.Propose("SET key val")
                              │ (only if Leader)
                              ▼
                         RaftNode appends to log
                         Replicates to peers via AppendEntries
                              │
                              ▼
                         applyCh ← "SET key val"
                              │
                              ▼
                         main.go committer loop
                         → db.Set(key, val)
                              │
                              ▼
                         DB: Encode → append to .db file
                         Update keyDir[key] in RAM
```

### GET (Read Path)
```
Client  ──GET key──►  TCPGateway.handleConn()
                           │
                           ▼
                      parser.Parse("GET key")
                           │
                           ▼
                      db.Get(key)  ← Direct local read (no Raft)
                           │
                           ▼
                      keyDir lookup → ReadAt(offset) → Decode
                      Check for tombstone → return value or error
```

### DEL (Delete Path)
```
Client  ──DEL key──►  TCPGateway.handleConn()
                           │
                           ▼
                      parser.Parse("DEL key")
                           │
                           ▼
                      raft.Propose("DEL key")
                           │ (only if Leader)
                           ▼
                      applyCh ← "DEL key"
                           │
                           ▼
                      main.go committer loop
                      → db.Delete(key)
                      → db.Set(key, "__PIZZADB_TOMBSTONE__")
                           │
                           ▼
                      keyDir[key] now points to tombstone
                      Future Get(key) returns "key not found"
```

---

## Authentication Architecture

### Token Storage
- Tokens are stored in a thread-safe `map[string]bool` inside `AuthManager` (`server/auth.go`).
- Default token: `pizzadb-default-token-2026` (configurable via `-tokens` CLI flag).
- Multiple tokens: Pass comma-separated values: `-tokens "token1,token2,token3"`.

### Thread Safety
- `ValidateToken()` uses `sync.RWMutex.RLock()` — multiple goroutines can validate concurrently.
- `AddToken()` / `RemoveToken()` use `sync.RWMutex.Lock()` — exclusive access for writes.
- This design is ready for a future admin control-plane that dynamically syncs tokens from the Next.js cloud dashboard without process restarts.

### Connection Lifecycle
```
TCP Connect → AUTH required → Command Loop → TCP Disconnect
     │              │               │
     │       Reject if bad token    │
     │       Allow PING pre-auth    │
     │                              │
     │                    Parse → Dispatch → Respond
```

---

## CLI Flags

```bash
go run . \
  -id localhost:8001 \
  -peers localhost:8002,localhost:8003 \
  -tokens "pizzadb-default-token-2026,another-secret-token"
```

| Flag | Default | Description |
|------|---------|-------------|
| `-id` | `localhost:8001` | This node's Raft address |
| `-peers` | `localhost:8002,localhost:8003` | Comma-separated peer Raft addresses |
| `-tokens` | `pizzadb-default-token-2026` | Comma-separated API tokens for gateway auth |

---

## Known Limitations & Next Steps

1. **Raft auto-commit:** `Propose()` currently auto-commits locally without waiting for quorum confirmation (demo mode from Phase 4). Needs proper majority-commit tracking.
2. **Leader redirect:** `SET`/`DEL` on a follower returns `-ERR not leader` but doesn't tell the client *who* the leader is. Future: return `-ERR not leader, try <leader_addr>`.
3. **No TLS:** The gateway runs over plain TCP. For production DBaaS, TLS termination is required.
4. **Token persistence:** Tokens are in-memory only. A future phase should persist them to disk or sync from the cloud dashboard.
5. **No compaction:** Tombstones accumulate on disk forever. A background compaction process should eventually reclaim space.
6. **GET consistency:** Reads go directly to local storage (stale reads possible on followers). Future: add linearizable read option via Raft.

---

## Build & Test Commands

```bash
# Build the entire project (from project root):
cd /home/parth/DBdev/PizzaDB
go build ./...

# Run a 3-node cluster:
# Terminal 1:
go run . -id localhost:8001 -peers localhost:8002,localhost:8003

# Terminal 2:
go run . -id localhost:8002 -peers localhost:8001,localhost:8003

# Terminal 3:
go run . -id localhost:8003 -peers localhost:8001,localhost:8002

# Test the PizzaQL gateway (connect to the leader node's gateway port):
# Terminal 4:
nc localhost 13001
AUTH pizzadb-default-token-2026
PING
SET hero Batman
SET user:1001 "{\"name\": \"Parth Bhanti\", \"role\": \"Chef\"}"
GET hero
GET user:1001
DEL hero
GET hero
```

---

## Phase 1: Containerization & Architecture Deployment

> **Added:** 2026-06-13
> **Target Infra:** 3× Oracle Cloud Infrastructure (OCI) Always-Free Ampere A1 (ARM64)

### Multi-Stage Docker Strategy

The `Dockerfile` uses a two-stage build to produce a minimal production image:

```
┌─────────────────────────────────────────────────────┐
│  Stage 1: golang:1.22-alpine (Builder)              │
│  • Copies go.mod first for layer caching            │
│  • CGO_ENABLED=0 → fully static binary              │
│  • -ldflags="-s -w" → strips symbols (~32% smaller) │
│  • Output: /pizzadb (single binary)                 │
└──────────────────────┬──────────────────────────────┘
                       │ COPY --from=builder
                       ▼
┌─────────────────────────────────────────────────────┐
│  Stage 2: alpine:latest (Runner)                    │
│  • ~7 MB base image                                 │
│  • Non-root user: pizzadb:pizzadb                   │
│  • /data volume for .db files                       │
│  • EXPOSE 8001 (Raft) + 13001 (Gateway)             │
│  • ENTRYPOINT ["pizzadb"] + CMD defaults             │
└─────────────────────────────────────────────────────┘
```

### File Additions

| File            | Purpose                                                    |
| --------------- | ---------------------------------------------------------- |
| `Dockerfile`    | Multi-stage production build (builder → alpine runner)     |
| `.dockerignore` | Excludes .db files, test clients, docs, Go tarball from context |

### Cross-Compilation & Push Commands

```bash
# 1. Create a buildx builder (one-time setup)
docker buildx create --name pizzabuilder --use

# 2. Build for ARM64, tag, and push to Docker Hub in one command
docker buildx build \
  --platform linux/arm64 \
  -t parthbhanti22/pizzadb:latest \
  --push \
  .

# Alternative: Build and load locally (for local testing on ARM64 or emulated)
docker buildx build \
  --platform linux/arm64 \
  -t parthbhanti22/pizzadb:latest \
  --load \
  .
```

### Container Runtime

On each OCI instance, pull and run:

```bash
# Node 1 (replace IPs with actual OCI private/public IPs)
docker run -d \
  --name pizzadb-node1 \
  --network host \
  -v /opt/pizzadb/data:/data \
  parthbhanti22/pizzadb:latest \
  -id <NODE1_IP>:8001 \
  -peers <NODE2_IP>:8001,<NODE3_IP>:8001 \
  -tokens "your-production-api-token"
```

**Key runtime notes:**
- `--network host` is used so Raft nodes can reach each other by IP without Docker NAT.
- `-v /opt/pizzadb/data:/data` persists the `.db` files across container restarts.
- The binary runs as non-root user `pizzadb` inside the container.
- Override `CMD` defaults by appending `-id`, `-peers`, `-tokens` flags after the image name.

### Port Exposure (Per Container)

| Port  | Protocol | Direction | Purpose                        |
| ----- | -------- | --------- | ------------------------------ |
| 8001  | TCP      | Internal  | Raft RPC (node ↔ node)         |
| 13001 | TCP      | External  | PizzaQL Gateway (client → node)|

### OCI Networking Requirements

> **⚠️ CRITICAL — Note for Next Phase Deployment:**
>
> The Oracle Virtual Cloud Network (VCN) ingress rules must be modified BEFORE the cluster can form. By default, OCI security lists block all inbound traffic except SSH (22).
>
> **Required VCN Security List Ingress Rules:**
>
> | Port  | Protocol | Source CIDR   | Purpose                              |
> | ----- | -------- | ------------- | ------------------------------------ |
> | 8001  | TCP      | 10.0.0.0/16   | Raft inter-node (private subnet only)|
> | 13001 | TCP      | 0.0.0.0/0     | PizzaQL Gateway (public access)      |
>
> - Port 8001 should be restricted to the VCN's private subnet CIDR (e.g., `10.0.0.0/16`) — Raft traffic should NEVER be exposed to the public internet.
> - Port 13001 can be opened to `0.0.0.0/0` for public client access, or restricted to specific client CIDRs.
> - Additionally, each instance's `iptables` (or `firewalld`) must also allow these ports if the OS-level firewall is active.

### Image Size Estimate

| Component        | Size     |
| ---------------- | -------- |
| alpine:latest    | ~7 MB    |
| PizzaDB binary   | ~4.5 MB  |
| ca-certs + tzdata| ~2 MB    |
| **Total image**  | **~14 MB** |

---

## Benchmark Results

> **Added:** 2026-07-11
> **Hardware:** Intel i3-1005G1 @ 1.20GHz, 4 threads, WSL2 Linux
> **Test file:** `db_test.go`
> **Command:** `go test -bench=. -benchmem -count=3`

### Storage Engine Performance (Single-Node, No Raft)

| Benchmark | Ops/sec | ns/op | B/op | Allocs/op |
|-----------|---------|-------|------|-----------|
| BenchmarkSet | ~250,000 | ~4,000 | 419-433 | 6 |
| BenchmarkGet | ~1,000,000 | ~1,000 | 85 | 4 |
| BenchmarkSetGet | ~200,000 | ~5,000 | 490-562 | 9 |
| BenchmarkParallelSet | ~140,000 | ~7,000 | 331-348 | 5-6 |
| BenchmarkParallelGet | ~1,500,000 | ~700 | 85 | 4 |
| BenchmarkDelete | ~300,000 | ~3,200 | 279 | 3 |
| BenchmarkRecover10k | ~60/sec | ~17ms | 1.6MB | 20,218 |

### Binary Size Comparison (Measured)

| Build Config | Size | Reduction |
|-------------|------|-----------|
| `go build .` | 6,976,332 bytes (6.7 MB) | — |
| `CGO_ENABLED=0 go build -ldflags="-s -w" .` | 4,726,936 bytes (4.5 MB) | **32.2%** |

### Correctness Tests

| Test | Status | Description |
|------|--------|-------------|
| TestRecover10kEntries | ✅ PASS | Write 10k → crash → reopen → verify all 10k |
| TestDeleteTombstone | ✅ PASS | Tombstones persist across crash recovery |
| TestConcurrentSafety | ✅ PASS | 30 goroutines (10W + 20R) × 1,000 ops each |

### How to Reproduce

```bash
cd /home/parth/DBdev/PizzaDB

# Unit tests + correctness
go test -v

# Full benchmarks (3 runs for stability)
go test -bench=. -benchmem -count=3

# Race detector validation
go test -v -race

# Binary size comparison
go build -o pizzadb_full .
CGO_ENABLED=0 go build -ldflags="-s -w" -o pizzadb_stripped .
ls -lh pizzadb_full pizzadb_stripped
```

