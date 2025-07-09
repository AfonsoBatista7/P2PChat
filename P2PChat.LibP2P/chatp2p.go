package main

import (
	"crypto/rand"
	"flag"
	"os"
)

func main() {
	// Parse command line arguments
	port := flag.String("port", "8080", "Port to listen on")
	flag.Parse()

	// Use environment variable PORT if set, otherwise use command line argument
	if envPort := os.Getenv("PORT"); envPort != "" {
		port = &envPort
	}

	// Log the port being used
	logger := NewLogger()

	// Initialize PeerManager
	pm := &PeerManager{
		done: make(chan bool),
	}

	// Initialize host (P2P node)
	hostData, err := MakeHost(rand.Reader)
	if err != nil {
		logger.LogToFrontend("ERROR", "Failed to create host: %s", err)
		return
	}

	logger.LogToFrontend("INFO", "Host created: %s", hostData.ID())

	// Initialize ConnectionManager
	connectionManager := NewConnectionManager(logger, hostData)
	connectionManager.SetContext(nil) // Set context as needed

	// Initialize APIServer
	apiServer := NewAPIServer(pm, connectionManager, logger, hostData)
	apiServer.StartHTTPServer(*port)

	// Keep the main goroutine alive
	select {}
}
