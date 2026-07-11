package main

import (
	"flag"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	gateway "github.com/parthbhanti22/PizzaDB/server"
)

func main() {
	// 1. Parse Command Line Arguments
	id := flag.String("id", "localhost:8001", "My ID (Address)")
	peersStr := flag.String("peers", "localhost:8002,localhost:8003", "Comma-separated peers")
	tokensStr := flag.String("tokens", "pizzadb-default-token-2026", "Comma-separated API tokens for the PizzaQL gateway")
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

	// 4. Start Committer Loop (handles both SET and DEL from Raft log)
	go func() {
		for msg := range applyCh {
			parts := strings.SplitN(msg, " ", 3)
			if len(parts) < 2 {
				continue
			}

			op := strings.ToUpper(parts[0])
			switch op {
			case "SET":
				if len(parts) >= 3 {
					k, v := parts[1], parts[2]
					db.Set(k, v)
					fmt.Printf("✅ [%s] Committed to Disk: SET %s %s\n", *id, k, v)
				}
			case "DEL":
				k := parts[1]
				db.Delete(k)
				fmt.Printf("🗑️  [%s] Committed to Disk: DEL %s\n", *id, k)
			}
		}
	}()

	// 5. Start PizzaQL TCP Gateway (plain-text protocol with auth)
	tokens := strings.Split(*tokensStr, ",")
	auth := gateway.NewAuthManager(tokens)
	gatewayPort := fmt.Sprintf(":%d", raftPort+5000)

	gw := gateway.NewTCPGateway(db, raft, auth, gatewayPort)
	go func() {
		if err := gw.ListenAndServe(); err != nil {
			fmt.Printf("❌ Gateway error: %v\n", err)
		}
	}()

	// 6. Start legacy binary-protocol server (blocks main)
	fmt.Printf("🔥 PizzaDB Listening for Clients on %s\n", dbPort)
	fmt.Printf("🌐 PizzaQL Gateway available on %s\n", gatewayPort)
	server.Start(dbPort)
}