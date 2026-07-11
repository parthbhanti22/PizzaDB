package server

import (
	"bufio"
	"fmt"
	"net"
	"strings"

	"github.com/parthbhanti22/PizzaDB/parser"
)

// DB is the interface that the gateway uses to read from the storage layer.
// This avoids a circular import — main.go's *DB satisfies this interface.
type DB interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

// Raft is the interface that the gateway uses to propose writes.
// main.go's *RaftNode satisfies this interface.
type Raft interface {
	Propose(command string) bool
}

// TCPGateway is the client-facing plain-text TCP server for the DBaaS
// data plane. It speaks the PizzaQL line protocol over newline-delimited
// TCP sockets with token-based authentication.
type TCPGateway struct {
	db       DB
	raft     Raft
	auth     *AuthManager
	listener net.Listener
	addr     string
}

// NewTCPGateway creates a new gateway instance.
func NewTCPGateway(db DB, raft Raft, auth *AuthManager, addr string) *TCPGateway {
	return &TCPGateway{
		db:   db,
		raft: raft,
		auth: auth,
		addr: addr,
	}
}

// ListenAndServe starts the TCP listener and enters the accept loop.
// This call blocks — launch it in a goroutine from main.go.
func (gw *TCPGateway) ListenAndServe() error {
	ln, err := net.Listen("tcp", gw.addr)
	if err != nil {
		return fmt.Errorf("tcp gateway listen error: %v", err)
	}
	gw.listener = ln

	fmt.Printf("🌐 PizzaQL Gateway listening on %s\n", gw.addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			// Check if listener was closed intentionally
			if strings.Contains(err.Error(), "use of closed network connection") {
				return nil
			}
			fmt.Printf("Gateway accept error: %v\n", err)
			continue
		}
		go gw.handleConn(conn)
	}
}

// Close shuts down the gateway listener gracefully.
func (gw *TCPGateway) Close() error {
	if gw.listener != nil {
		return gw.listener.Close()
	}
	return nil
}

// handleConn manages a single client connection's lifecycle.
// The first command MUST be AUTH. After authentication, the connection
// enters an interactive command loop until the client disconnects.
func (gw *TCPGateway) handleConn(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	// Increase max token size for large JSON values (1 MB)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	remoteAddr := conn.RemoteAddr().String()
	authenticated := false

	for scanner.Scan() {
		line := scanner.Text()

		// Parse the raw line into a structured command
		cmd, err := parser.Parse(line)
		if err != nil {
			writeLine(conn, "-ERR %s", err.Error())
			continue
		}

		// ── Auth Gate ──────────────────────────────────────────────
		// Before authentication, only AUTH and PING are allowed.
		if !authenticated {
			if cmd.Op == "AUTH" {
				if gw.auth.ValidateToken(cmd.Value) {
					authenticated = true
					fmt.Printf("🔑 [%s] Authenticated successfully\n", remoteAddr)
					writeLine(conn, "+OK Authenticated")
				} else {
					fmt.Printf("🚫 [%s] Auth failed (invalid token)\n", remoteAddr)
					writeLine(conn, "-ERR invalid token")
				}
				continue
			}
			// Allow PING even without auth (health checks)
			if cmd.Op == "PING" {
				writeLine(conn, "+PONG")
				continue
			}
			writeLine(conn, "-ERR not authenticated. Send AUTH <token> first")
			continue
		}

		// ── Command Dispatch (post-auth) ──────────────────────────
		switch cmd.Op {
		case "PING":
			writeLine(conn, "+PONG")

		case "AUTH":
			// Already authenticated on this connection
			writeLine(conn, "+OK already authenticated")

		case "SET":
			command := fmt.Sprintf("SET %s %s", cmd.Key, cmd.Value)
			if gw.raft.Propose(command) {
				writeLine(conn, "+OK")
			} else {
				writeLine(conn, "-ERR not leader")
			}

		case "GET":
			value, err := gw.db.Get(cmd.Key)
			if err != nil {
				writeLine(conn, "-ERR %s", err.Error())
			} else {
				writeLine(conn, "+OK %s", value)
			}

		case "DEL":
			command := fmt.Sprintf("DEL %s", cmd.Key)
			if gw.raft.Propose(command) {
				writeLine(conn, "+OK")
			} else {
				writeLine(conn, "-ERR not leader")
			}

		default:
			writeLine(conn, "-ERR unknown command: %s", cmd.Op)
		}
	}

	// Scanner exited — client disconnected or read error
	if err := scanner.Err(); err != nil {
		fmt.Printf("⚠️  [%s] Read error: %v\n", remoteAddr, err)
	}
}

// writeLine sends a formatted line terminated by \r\n to the client.
// Uses CRLF for maximum compatibility with telnet, netcat, and custom clients.
func writeLine(conn net.Conn, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	conn.Write([]byte(msg + "\r\n"))
}
