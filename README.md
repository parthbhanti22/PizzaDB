# PizzaDB

[![PizzaDB CI](https://github.com/parthbhanti22/PizzaDB/actions/workflows/go.yml/badge.svg)](https://github.com/parthbhanti22/PizzaDB/actions/workflows/go.yml)
![Go Version](https://img.shields.io/badge/go-1.22+-00ADD8?style=flat&logo=go)
![Architecture](https://img.shields.io/badge/architecture-distributed-orange)
![License](https://img.shields.io/badge/license-MIT-green)

**PizzaDB** is a distributed, fault-tolerant key-value storage engine built from scratch in Go.

It implements the **Raft Consensus Algorithm** to guarantee strong consistency (CP) across a cluster of nodes. Unlike standard educational projects, PizzaDB features a custom binary TCP wire protocol, a persistent Log-Structured Merge (LSM) tree storage engine, and a chaos-engineering suite for validating resilience against network partitions and node crashes.

---

## 📸 Chaos Resilience Demo

*The screenshot below demonstrates the system's ability to auto-recover. When the Leader node (running on port 9001) is killed mid-operation, the cluster detects the failure, holds an election, and seamlessly redirects traffic to the new Leader (port 9002) with zero data loss.*

![Chaos Monkey Test Result](crashtest.PNG)


---

## 🏗 System Architecture
![RAG Architecture Diagram](architecture.png)

PizzaDB is composed of three distinct, tightly coupled layers:

### 1. The Consensus Layer - Raft Implementation (In Orange)
The core brain of the system. It ensures that all nodes in the cluster agree on the state of the data, even in the event of failures.
* **Leader Election:** Nodes use randomized election timeouts (150-300ms) to detect leader failures.
* **Log Replication:** The Leader appends client commands to its local log and broadcasts `AppendEntries` RPCs to followers.
* **Safety:** Entries are only committed to the storage engine once a quorum (N/2 + 1) of nodes have acknowledged receipt.

### 2. The Storage Layer - LSM Tree (In Green)
A persistent storage engine modeled after Bitcask and Log-Structured Merge Trees.
* **In-Memory Index:** A generic Hash Map maintains pointers (offsets) to data on disk for O(1) read performance.
* **Append-Only Log:** All writes are serialized and appended to the end of a binary file, ensuring sequential write performance and crash recovery.
* **Binary Serialization:** Custom encoder/decoder handles variable-length keys and values with header metadata.

### 3. The Networking Layer - Custom Protocol (In Blue)
Instead of HTTP/JSON, PizzaDB uses a custom binary protocol over raw TCP sockets for maximum throughput.
* **Packet Structure:** `[Command (1B)] [KeyLen (4B)] [ValLen (4B)] [Key Payload] [Value Payload]`
* **Connection Pooling:** The Raft internal transport maintains persistent TCP connections between peers to minimize handshake latency during heartbeats.

---

## 🚀 Getting Started

### Prerequisites
* Go 1.22 or higher
* Linux/WSL2 environment (Recommended for network testing)

### Installation
```bash
git clone [https://github.com/parthbhanti22/PizzaDB.git](https://github.com/parthbhanti22/PizzaDB.git)
cd PizzaDB
go mod tidy
```

# 🚀 Running a Local Cluster

To simulate a **3-node distributed cluster** on a single machine, run the following commands in three separate terminal windows.

### 🖥️ Terminal 1 (Node A - Leader Candidate)
```bash
go run . -id localhost:8001 -peers localhost:8002,localhost:8003
```

### 🖥️ Terminal 2 (Node B)
```bash
go run . -id localhost:8002 -peers localhost:8001,localhost:8003
```

### 🖥️ Terminal 3 (Node C)
```bash
go run . -id localhost:8003 -peers localhost:8001,localhost:8002
```

# 🧪 Testing
## 1. Functional Client Test
### Run the basic client to verify SET/GET operations:
```bash
go run client/main.go
```
## 2. The "Chaos Monkey" Stress Test
### This script simulates a high-traffic environment while handling dynamic leader failover.

Ensure all 3 cluster nodes are running.
Run the chaos script:

```bash
go run client/chaos.go
```
Kill the Leader: Find the terminal running the current Leader and press Ctrl+C.

Observe: The chaos script will briefly report Cluster Down, then automatically discover the new Leader and resume writing.
