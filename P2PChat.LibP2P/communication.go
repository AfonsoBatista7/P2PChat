package main

import (
	"context"
	"net/http"
	"time"
)

// CommunicationHandler defines the interface for different communication protocols
type CommunicationHandler interface {
	// Start begins listening for requests using the given PeerService
	Start(service PeerService) error

	// Stop shuts down the communication handler
	Stop() error

	// GetProtocolName returns the name of the communication protocol
	GetProtocolName() string
}

// CommunicationConfig holds configuration for communication handlers
type CommunicationConfig struct {
	Port string
}

// HTTPCommunicationHandler implements CommunicationHandler for HTTP REST API
type HTTPCommunicationHandler struct {
	httpHandler *HTTPRequestHandler
	server      *http.Server
	config      *CommunicationConfig
}

// NewHTTPCommunicationHandler creates a new HTTP communication handler
func NewHTTPCommunicationHandler(config *CommunicationConfig) CommunicationHandler {
	return &HTTPCommunicationHandler{
		config: config,
	}
}

// Start begins listening for HTTP requests
func (h *HTTPCommunicationHandler) Start(service PeerService) error {
	// Create HTTP request handler that wraps the PeerService
	h.httpHandler = NewAPIServerWithService(service)

	// Create HTTP server
	h.server = &http.Server{
		Addr:         ":" + h.config.Port,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Set up routes using the HTTP request handler
	h.httpHandler.SetupRoutes()

	// Start server in goroutine
	go func() {
		if err := h.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Log error but don't crash - this will be handled by the main error handling
		}
	}()

	return nil
}

// Stop shuts down the HTTP server gracefully
func (h *HTTPCommunicationHandler) Stop() error {
	if h.server == nil {
		return nil
	}

	// Create a context with timeout for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt graceful shutdown
	return h.server.Shutdown(ctx)
}

// GetProtocolName returns the protocol name
func (h *HTTPCommunicationHandler) GetProtocolName() string {
	return "HTTP"
}

// Example of how you could add other protocols:

// GRPCCommunicationHandler would implement CommunicationHandler for gRPC
// type GRPCCommunicationHandler struct {
//     server *grpc.Server
//     config *CommunicationConfig
// }

// WebSocketCommunicationHandler would implement CommunicationHandler for WebSockets
// type WebSocketCommunicationHandler struct {
//     upgrader websocket.Upgrader
//     config   *CommunicationConfig
// }
