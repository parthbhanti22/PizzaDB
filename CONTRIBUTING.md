# Contributing to PizzaDB

First off, thank you for considering contributing to PizzaDB.

This project is a high-performance distributed system. We prioritize **correctness** and **safety** (CP) over features. As such, the bar for code quality is high.

## 📉 The Process
1. **Open an Issue First:** Do not open a massive PR without discussing it first. If you find a bug or want to optimize the TCP transport, open an issue explaining the "Why."
2. **One Logical Change:** Keep PRs small and focused.
3. **Tests are Mandatory:** Any PR that touches logic (Raft, LSM, Networking) must include a reproduction test case or a benchmark proving the improvement.

## 🛠 Development Guidelines
* **Race Conditions:** We use `go test -race` to detect data races. If your PR introduces a race, it will be rejected.
* **Raft Safety:** If you modify `raft.go`, you must prove that your change does not violate the Raft safety properties (Log Matching, Leader Completeness).
* **No External Dependencies:** We avoid heavy frameworks. Use the Go standard library (`net`, `sync`, `io`) wherever possible.

## 🧪 Running Tests
Before submitting, ensure all tests pass locally:
```bash
go test -v -race ./...
