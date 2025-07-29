package main

import (
	"crypto/rand"
	"flag"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// Parse command line arguments
	port := flag.String("port", "8080", "Port to listen on")
	flag.Parse()

	// Use environment variable PORT if set, otherwise use command line argument
	if envPort := os.Getenv("PORT"); envPort != "" {
		port = &envPort
	}

	logger := NewLogger()

	// Initialize PeerManager
	pm := &PeerManager{
		done:     make(chan bool),
		dhtReady: make(chan bool),
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

	// Create PeerService (core P2P logic abstraction)
	peerService := NewPeerService(pm, connectionManager, logger, hostData)

	// Create communication configuration
	commConfig := &CommunicationConfig{
		Port: *port,
	}

	// Create HTTP communication handler (pluggable)
	commHandler := NewHTTPCommunicationHandler(commConfig)

	// Start the communication handler with the peer service
	logger.LogToFrontend("INFO", "Starting %s communication handler on port %s",
		commHandler.GetProtocolName(), *port)

	err = commHandler.Start(peerService)
	if err != nil {
		logger.LogToFrontend("ERROR", "Failed to start communication handler: %v", err)
		return
	}

	logger.LogToFrontend("INFO", "Application started successfully. Press Ctrl+C to stop.")

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)

	// Wait for shutdown signal
	sig := <-sigChan
	logger.LogToFrontend("INFO", "Received signal: %v. Initiating graceful shutdown...", sig)

	// Graceful shutdown sequence
	logger.LogToFrontend("INFO", "Stopping communication handler...")
	if err := commHandler.Stop(); err != nil {
		logger.LogToFrontend("ERROR", "Error stopping communication handler: %v", err)
	} else {
		logger.LogToFrontend("INFO", "Communication handler stopped successfully")
	}

	logger.LogToFrontend("INFO", "Closing P2P service...")
	if err := peerService.Close(); err != nil {
		logger.LogToFrontend("ERROR", "Error closing P2P service: %v", err)
	} else {
		logger.LogToFrontend("INFO", "P2P service closed successfully")
	}

	logger.LogToFrontend("INFO", "Graceful shutdown completed")
}
