package main

import(
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

//server holds the db and the listener

type Server struct {
	db   *DB 
	raft *RaftNode
}

func NewServer(db *DB, raft *RaftNode) *Server {
	return &Server{db: db, raft: raft}
}

//start opens the port and waits for connections
func (s *Server) Start(port string) {
	ln, err := net.Listen("tcp", port)
	if err != nil {
		fmt.Printf("Error starting server: %v\n",err)
		return
	}
	defer ln.Close()

	fmt.Printf("🚀 Pizza Server Listening on %s\n",port)

	for {
		//accept new connections (Blocking call)
		conn, err := ln.Accept()
		if err != nil {
			fmt.Printf("Connection error: %v\n",err)
			continue
		}

		//handle the connection in a new goroutine (Thread)
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	for {
		// 1. Read the Command Byte (1 byte)
		cmdBuf := make([]byte, 1)
		_, err := conn.Read(cmdBuf)
		if err != nil {
			if err != io.EOF {
				fmt.Printf("Read error: %v\n", err)
			}
			return // Client disconnected
		}
		cmd := cmdBuf[0]

		// 2. Switch based on Command
		switch cmd {
		case 0x01: // SET
			s.handleSet(conn)
		case 0x02: // GET
			s.handleGet(conn)
		default:
			fmt.Println("Unknown command")
			return
		}
	}
}

func (s *Server) handleSet(conn net.Conn) {
	// Protocol: [KeyLen(4)] [ValLen(4)] [Key] [Value]
	
	// Read Header (8 bytes)
	header := make([]byte, 8)
	_, err := io.ReadFull(conn, header)
	if err != nil {
		return
	}

	keyLen := binary.LittleEndian.Uint32(header[0:4])
	valLen := binary.LittleEndian.Uint32(header[4:8])

	// Read Body
	body := make([]byte, keyLen+valLen)
	_, err = io.ReadFull(conn, body)
	if err != nil {
		return
	}

	key := string(body[:keyLen])
	value := string(body[keyLen:])

	command := fmt.Sprintf("SET %s %s", key, value)
	success := s.raft.Propose(command)
	
	// Send Response (0x00 = Success, 0x01 = Error)
	if !success {
		conn.Write([]byte{0x01}) 
	} else {
		conn.Write([]byte{0x00})
	}
}

func (s *Server) handleGet(conn net.Conn) {
	// Protocol: [KeyLen(4)] [Key]
	
	// Read Key Size
	lenBuf := make([]byte, 4)
	_, err := io.ReadFull(conn, lenBuf)
	if err != nil {
		return
	}
	keyLen := binary.LittleEndian.Uint32(lenBuf)

	// Read Key
	keyBuf := make([]byte, keyLen)
	_, err = io.ReadFull(conn, keyBuf)
	if err != nil {
		return
	}
	key := string(keyBuf)

	// Execute
	value, err := s.db.Get(key)
	
	if err != nil {
		// Error: Send [0x01] [0 length]
		conn.Write([]byte{0x01})
		binary.Write(conn, binary.LittleEndian, uint32(0))
		return
	}

	// Success: Send [0x00] [ValLen] [Value]
	conn.Write([]byte{0x00})
	binary.Write(conn, binary.LittleEndian, uint32(len(value)))
	conn.Write([]byte(value))
}