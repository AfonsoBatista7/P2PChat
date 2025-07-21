package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
)

// APIServer handles all HTTP API operations
type APIServer struct {
	peerManager       *PeerManager
	connectionManager *ConnectionManager
	logger            *Logger
	host              interface{} // Will be properly typed when host is available
	hostData          host.Host
}

// NewAPIServer creates a new API server
func NewAPIServer(peerManager *PeerManager, connectionManager *ConnectionManager, logger *Logger, hostData host.Host) *APIServer {
	return &APIServer{
		peerManager:       peerManager,
		connectionManager: connectionManager,
		logger:            logger,
		hostData:          hostData,
	}
}

// SetHost sets the host for status operations
func (as *APIServer) SetHost(host interface{}) {
	as.host = host
}

// StartHTTPServer starts the HTTP server
func (as *APIServer) StartHTTPServer(port string) {
	http.HandleFunc("/api/start", as.handleStart)
	http.HandleFunc("/api/connect", as.handleConnect)
	http.HandleFunc("/api/send", as.handleSend)
	http.HandleFunc("/api/close", as.handleClose)
	http.HandleFunc("/api/status", as.handleStatus)
	http.HandleFunc("/api/discover", as.handleDiscover)
	http.HandleFunc("/api/logs", as.handleLogs)

	server := &http.Server{
		Addr: ":" + port,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			as.logger.LogToFrontend("ERROR", "HTTP server failed: %v", err)
		}
	}()
}

func (as *APIServer) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req APIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		as.logger.LogToFrontend("ERROR", "Invalid request body: %v", err)
		as.sendJSONResponse(w, APIResponse{Success: false, Error: "Invalid request body"})
		return
	}

	go func() {
		as.peerManager.StartProtocolP2P([]string{req.Bootstrap}, req.Debug, req.PeerID, as.hostData, as.logger, as.connectionManager)
	}()

	as.sendJSONResponse(w, APIResponse{Success: true, Message: "P2P network started"})
}

func (as *APIServer) handleClose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Close all network connections properly
	as.logger.LogToFrontend("INFO", "Closing all network connections...")

	// Close all connections in the connection manager
	connections := as.connectionManager.GetConnections()
	for _, conn := range connections {
		as.connectionManager.removeConnection(conn)
	}

	// Clean up DHT advertisements
	if dht := as.peerManager.GetDHT(); dht != nil {
		as.peerManager.cleanupDHT(dht, as.logger)
	}

	// Close the host to disconnect from relay
	if as.hostData != nil {
		as.logger.LogToFrontend("INFO", "Disconnecting from relay...")
		as.hostData.Close()
	}

	// Signal done after closing connections
	as.sendJSONResponse(w, APIResponse{Success: true, Message: "Peer connection closed"})
	as.peerManager.done <- true
}

func (as *APIServer) handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req APIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		as.sendJSONResponse(w, APIResponse{Success: false, Error: "Invalid request body"})
		return
	}

	if req.PeerID == "" {
		as.sendJSONResponse(w, APIResponse{Success: false, Error: "PeerID is required"})
		return
	}

	// Use connectionManager to connect to peer
	// TODO: Convert req.PeerID (string) to peer.ID as needed
	// Example: peerID, err := peer.Decode(req.PeerID)
	// Then: as.connectionManager.ConnectToPeer(peerID)

	as.sendJSONResponse(w, APIResponse{Success: true, Message: "Connection attempt initiated"})
}

func (as *APIServer) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req APIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		as.sendJSONResponse(w, APIResponse{Success: false, Error: "Invalid request body"})
		return
	}

	if req.Message == "" {
		as.sendJSONResponse(w, APIResponse{Success: false, Error: "Message is required"})
		return
	}

	// Use connectionManager to send data to all peers
	as.peerManager.publishMessage(r.Context(), req.Message, as.logger)
	as.sendJSONResponse(w, APIResponse{Success: true, Message: "Message sent"})
}

func (as *APIServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	connectionCount := len(as.connectionManager.GetConnections())

	status := map[string]interface{}{
		"connected": connectionCount > 0,
		"peers":     connectionCount,
		"hasPeers":  connectionCount > 0,
	}

	as.sendJSONResponse(w, APIResponse{Success: true, Message: "Status retrieved", Error: fmt.Sprintf("%v", status)})
}

func (as *APIServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	logChannel := as.logger.GetLogChannel()
	select {
	case logMsg := <-logChannel:
		data, err := json.Marshal(logMsg)
		if err != nil {
			as.logger.LogToFrontend("ERROR", "Failed to marshal log message: %v", err)
		}
		_, err = fmt.Fprintf(w, "data: %s\n\n", data)
		if err != nil {
			as.logger.LogToFrontend("ERROR", "Failed to write log message: %v", err)
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}

func (as *APIServer) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	as.logger.LogToFrontend("INFO", "Starting initial peer discovery...")

	// Wait for DHT to be ready (timeout after 10 seconds)
	if !as.peerManager.WaitForDHTReady(10 * time.Second) {
		as.sendJSONResponse(w, APIResponse{Success: false, Error: "DHT initialization timeout"})
		return
	}

	// Create discovery configuration
	config := DiscoveryConfig{
		RendezvousString:  RendezvousString,
		ConnectionTimeout: ConnectionTimeout,
		AdvertiseInterval: AdvertiseInterval,
		ValidationTimeout: 1 * time.Second,
		MaxConcurrent:     5,
	}

	// Create discovery manager
	discoveryManager := NewPeerDiscoveryManager(config, as.connectionManager, as.logger)

	// Get DHT from peer manager
	dht := as.peerManager.GetDHT()
	if dht == nil {
		as.sendJSONResponse(w, APIResponse{Success: false, Error: "DHT not initialized"})
		return
	}

	// Advertise our presence first
	ctx := context.Background()
	discovery := routing.NewRoutingDiscovery(dht)
	util.Advertise(ctx, discovery, RendezvousString)
	as.logger.LogToFrontend("INFO", "Advertised presence in DHT")

	// Give other peers a moment to advertise themselves
	time.Sleep(1 * time.Second)

	// Perform initial discovery
	result := discoveryManager.DiscoverPeers(ctx, as.hostData, dht)

	// Check connections after discovery and stream establishment
	connectionCount := len(as.connectionManager.GetConnections())

	// If no connections tracked but we had successful network connections,
	// count the network connections instead
	if connectionCount == 0 && result.SuccessCount > 0 {
		connectionCount = result.SuccessCount
	}

	as.logger.LogToFrontend("INFO", "Discovery completed - found %d connections", connectionCount)

	// Start background discovery process after initial discovery completes
	as.peerManager.StartBackgroundDiscovery()

	status := map[string]interface{}{
		"connected": connectionCount > 0,
		"peers":     connectionCount,
		"hasPeers":  connectionCount > 0,
	}

	as.sendJSONResponse(w, APIResponse{Success: true, Message: "Discovery completed", Error: fmt.Sprintf("%v", status)})
}

func (as *APIServer) sendJSONResponse(w http.ResponseWriter, response APIResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
