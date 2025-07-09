package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/libp2p/go-libp2p/core/host"
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

	status := map[string]interface{}{
		"connected": len(as.connectionManager.GetConnections()) > 0,
		"peers":     len(as.connectionManager.GetConnections()),
		// Remove hostData reference or replace with a proper field if needed
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

func (as *APIServer) sendJSONResponse(w http.ResponseWriter, response APIResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
