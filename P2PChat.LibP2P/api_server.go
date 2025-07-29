package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/libp2p/go-libp2p/core/host"
)

// HTTPRequestHandler handles HTTP-specific request/response translation
// It translates between HTTP and the protocol-agnostic PeerService
type HTTPRequestHandler struct {
	peerService PeerService
}

// NewHTTPRequestHandler creates a new HTTP request handler (legacy constructor for backward compatibility)
func NewAPIServer(peerManager *PeerManager, connectionManager *ConnectionManager, logger *Logger, hostData host.Host) *HTTPRequestHandler {
	peerService := NewPeerService(peerManager, connectionManager, logger, hostData)
	return &HTTPRequestHandler{
		peerService: peerService,
	}
}

// NewAPIServerWithService creates a new HTTP request handler with a PeerService
func NewAPIServerWithService(peerService PeerService) *HTTPRequestHandler {
	return &HTTPRequestHandler{
		peerService: peerService,
	}
}

// Protocol-agnostic business logic methods
// These can be called from any communication protocol

// ProcessStartRequest handles P2P network startup
func (h *HTTPRequestHandler) ProcessStartRequest(bootstrap string, debug bool, peerID string) error {
	return h.peerService.StartP2P(bootstrap, debug, peerID)
}

// ProcessSendRequest handles message sending
func (h *HTTPRequestHandler) ProcessSendRequest(message string) error {
	return h.peerService.SendMessage(message)
}

// ProcessConnectRequest handles peer connection
func (h *HTTPRequestHandler) ProcessConnectRequest(peerID string) error {
	return h.peerService.ConnectToPeer(peerID)
}

// ProcessDiscoverRequest handles peer discovery
func (h *HTTPRequestHandler) ProcessDiscoverRequest() (*PeerDiscoveryResult, error) {
	return h.peerService.DiscoverPeers()
}

// ProcessStatusRequest handles status retrieval
func (h *HTTPRequestHandler) ProcessStatusRequest() (bool, int, error) {
	return h.peerService.GetStatus()
}

// ProcessCloseRequest handles P2P network shutdown
func (h *HTTPRequestHandler) ProcessCloseRequest() error {
	return h.peerService.Close()
}

// GetLogChannel returns the log channel (protocol-agnostic)
func (h *HTTPRequestHandler) GetLogChannel() <-chan LogMessage {
	return h.peerService.GetLogChannel()
}

// HTTP-specific methods below (these translate HTTP ↔ business logic)

// SetupRoutes configures the HTTP routes
func (h *HTTPRequestHandler) SetupRoutes() {
	http.HandleFunc("/api/start", h.handleStart)
	http.HandleFunc("/api/connect", h.handleConnect)
	http.HandleFunc("/api/send", h.handleSend)
	http.HandleFunc("/api/close", h.handleClose)
	http.HandleFunc("/api/status", h.handleStatus)
	http.HandleFunc("/api/discover", h.handleDiscover)
	http.HandleFunc("/api/logs", h.handleLogs)
}

func (h *HTTPRequestHandler) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req APIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendJSONResponse(w, APIResponse{Success: false, Error: "Invalid request body"})
		return
	}

	go func() {
		h.ProcessStartRequest(req.Bootstrap, req.Debug, req.PeerID)
	}()

	h.sendJSONResponse(w, APIResponse{Success: true, Message: "P2P network started"})
}

func (h *HTTPRequestHandler) handleClose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := h.ProcessCloseRequest()
	if err != nil {
		h.sendJSONResponse(w, APIResponse{Success: false, Error: err.Error()})
		return
	}

	h.sendJSONResponse(w, APIResponse{Success: true, Message: "Peer connection closed"})
}

func (h *HTTPRequestHandler) handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req APIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendJSONResponse(w, APIResponse{Success: false, Error: "Invalid request body"})
		return
	}

	if req.PeerID == "" {
		h.sendJSONResponse(w, APIResponse{Success: false, Error: "PeerID is required"})
		return
	}

	err := h.ProcessConnectRequest(req.PeerID)
	if err != nil {
		h.sendJSONResponse(w, APIResponse{Success: false, Error: err.Error()})
		return
	}

	h.sendJSONResponse(w, APIResponse{Success: true, Message: "Connection attempt initiated"})
}

func (h *HTTPRequestHandler) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req APIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendJSONResponse(w, APIResponse{Success: false, Error: "Invalid request body"})
		return
	}

	if req.Message == "" {
		h.sendJSONResponse(w, APIResponse{Success: false, Error: "Message is required"})
		return
	}

	err := h.ProcessSendRequest(req.Message)
	if err != nil {
		h.sendJSONResponse(w, APIResponse{Success: false, Error: err.Error()})
		return
	}

	h.sendJSONResponse(w, APIResponse{Success: true, Message: "Message sent"})
}

func (h *HTTPRequestHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	connected, peers, err := h.ProcessStatusRequest()
	if err != nil {
		h.sendJSONResponse(w, APIResponse{Success: false, Error: err.Error()})
		return
	}

	status := map[string]interface{}{
		"connected": connected,
		"peers":     peers,
		"hasPeers":  connected,
	}

	h.sendJSONResponse(w, APIResponse{Success: true, Message: "Status retrieved", Error: fmt.Sprintf("%v", status)})
}

func (h *HTTPRequestHandler) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	logChannel := h.GetLogChannel()

	// Keep the connection alive and continuously stream logs
	for {
		select {
		case logMsg := <-logChannel:
			data, err := json.Marshal(logMsg)
			if err != nil {
				log.Printf("Failed to marshal log message: %v", err)
				continue
			}
			_, err = fmt.Fprintf(w, "data: %s\n\n", data)
			if err != nil {
				// Client disconnected
				return
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		case <-r.Context().Done():
			// Client disconnected
			return
		}
	}
}

func (h *HTTPRequestHandler) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result, err := h.ProcessDiscoverRequest()
	if err != nil {
		h.sendJSONResponse(w, APIResponse{Success: false, Error: err.Error()})
		return
	}

	status := map[string]interface{}{
		"connected": result.Connected,
		"peers":     result.Peers,
		"hasPeers":  result.HasPeers,
	}

	h.sendJSONResponse(w, APIResponse{Success: true, Message: "Discovery completed", Error: fmt.Sprintf("%v", status)})
}

func (h *HTTPRequestHandler) sendJSONResponse(w http.ResponseWriter, response APIResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
