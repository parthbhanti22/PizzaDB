package main

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"
	"math/rand"
)

func main() {
	// 1. Parse Command Line Arguments
	id := flag.String("id", "localhost:8001", "My ID (Address)")
	peersStr := flag.String("peers", "localhost:8002,localhost:8003", "Comma-separated peers")
	flag.Parse()

	peers := strings.Split(*peersStr, ",")

	// Seed random for election timers
	rand.Seed(time.Now().UnixNano())

	// 2. Start Raft RPC Server (This listens on localhost:8001)
	// 2. Setup Raft and Commit Channel
	fmt.Println("🚀 Starting Raft Node...")
	applyCh := make(chan string)
	raft := NewRaftNode(*id, peers, applyCh)
	raft.StartRPC() 
	go raft.Run()

	// 3. Start DB and Server
	raftPortStr := strings.Split(*id, ":")[1]
	raftPort, _ := strconv.Atoi(raftPortStr)
	dbPort := fmt.Sprintf(":%d", raftPort+1000)

	dbName := "pizza_" + raftPortStr + ".db"
	db, _ := NewDB(dbName)
	defer db.Close()
	
	server := NewServer(db, raft)

	// 4. Start Committer Loop
	go func() {
		for msg := range applyCh {
			parts := strings.Split(msg, " ")
			if len(parts) >= 3 {
					k, v := parts[1], parts[2]
					db.Set(k, v) 
					fmt.Printf("✅ [%s] Committed to Disk: %s %s\n", *id, k, v)
			}
		}
	}()
	
	// Blocks main
	fmt.Printf("🔥 PizzaDB Listening for Clients on %s\n", dbPort)
	server.Start(dbPort)
}