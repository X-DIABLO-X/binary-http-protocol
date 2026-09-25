package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"bhttp/pkg/bhttp"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <root_dir> [port]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s ./www 9000\n", os.Args[0])
		os.Exit(1)
	}

	rootDir := os.Args[1]
	port := 9000
	if len(os.Args) >= 3 {
		p, err := strconv.Atoi(os.Args[2])
		if err != nil || p <= 0 || p > 65535 {
			log.Fatalf("Invalid port number: %s\n", os.Args[2])
		}
		port = p
	}

	server, err := bhttp.NewServer(rootDir, port)
	if err != nil {
		log.Fatalf("Failed to initialize server: %v\n", err)
	}

	// Trap SIGINT / SIGTERM for graceful exit
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("\n[bserve] Shutting down gracefully...")
		_ = server.Stop()
		os.Exit(0)
	}()

	if err := server.Start(); err != nil {
		log.Fatalf("Server error: %v\n", err)
	}
}
