package main

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
)

// PeerService defines the core P2P operations that can be exposed through any communication protocol
type PeerService interface {
	// StartP2P initializes the P2P network with given parameters
	StartP2P(bootstrap string, debug bool, peerID string) error

	// SendMessage sends a message to all connected peers
	SendMessage(message string) error

	// ConnectToPeer attempts to connect to a specific peer
	ConnectToPeer(peerID string) error

	// DiscoverPeers performs peer discovery and returns connection info
	DiscoverPeers() (*PeerDiscoveryResult, error)

	// GetStatus returns the current connection status
	GetStatus() (connected bool, peers int, error error)

	// Close shuts down the P2P network
	Close() error

	// GetLogChannel returns a channel for receiving log messages
	GetLogChannel() <-chan LogMessage
}

// PeerDiscoveryResult contains the results of a peer discovery operation
type PeerDiscoveryResult struct {
	Connected    bool `json:"connected"`
	Peers        int  `json:"peers"`
	HasPeers     bool `json:"hasPeers"`
	SuccessCount int  `json:"successCount,omitempty"`
	FailureCount int  `json:"failureCount,omitempty"`
}

// ConcretePeerService implements PeerService using the existing P2P components
type ConcretePeerService struct {
	peerManager       *PeerManager
	connectionManager *ConnectionManager
	logger            *Logger
	hostData          host.Host
}

// NewPeerService creates a new concrete peer service
func NewPeerService(peerManager *PeerManager, connectionManager *ConnectionManager, logger *Logger, hostData host.Host) PeerService {
	return &ConcretePeerService{
		peerManager:       peerManager,
		connectionManager: connectionManager,
		logger:            logger,
		hostData:          hostData,
	}
}

// StartP2P initializes the P2P network
func (ps *ConcretePeerService) StartP2P(bootstrap string, debug bool, peerID string) error {
	// Validate parameters
	if bootstrap == "" {
		return fmt.Errorf("bootstrap address is required")
	}

	// Start the P2P protocol in a goroutine (non-blocking)
	go func() {
		ps.peerManager.StartProtocolP2P([]string{bootstrap}, debug, peerID, ps.hostData, ps.logger, ps.connectionManager)
	}()

	return nil
}

// SendMessage sends a message to all connected peers
func (ps *ConcretePeerService) SendMessage(message string) error {
	if message == "" {
		return fmt.Errorf("message is required")
	}

	ctx := context.Background()
	ps.peerManager.publishMessage(ctx, message, ps.logger)
	return nil
}

// ConnectToPeer attempts to connect to a specific peer
func (ps *ConcretePeerService) ConnectToPeer(peerID string) error {
	if peerID == "" {
		return fmt.Errorf("peerID is required")
	}

	ps.logger.LogToFrontend("INFO", "Connection attempt initiated for peer: %s", peerID)
	// TODO: Implement actual peer connection logic
	return nil
}

// DiscoverPeers performs peer discovery
func (ps *ConcretePeerService) DiscoverPeers() (*PeerDiscoveryResult, error) {
	ps.logger.LogToFrontend("INFO", "Starting peer discovery...")

	// Wait for DHT to be ready (timeout after 10 seconds)
	if !ps.peerManager.WaitForDHTReady(10 * time.Second) {
		return nil, fmt.Errorf("DHT initialization timeout")
	}

	// Get DHT from peer manager
	dht := ps.peerManager.GetDHT()
	if dht == nil {
		return nil, fmt.Errorf("DHT not initialized")
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
	discoveryManager := NewPeerDiscoveryManager(config, ps.connectionManager, ps.logger)

	// Advertise our presence first
	ctx := context.Background()
	discoveryInterface := routing.NewRoutingDiscovery(dht)
	util.Advertise(ctx, discoveryInterface, RendezvousString, discovery.TTL(AdvertisementTTL))
	ps.logger.LogToFrontend("INFO", "Advertised presence in DHT")

	// Give other peers a moment to advertise themselves
	time.Sleep(1 * time.Second)

	// Perform initial discovery
	result := discoveryManager.DiscoverPeers(ctx, ps.hostData, dht)

	// Check connections after discovery
	connectionCount := len(ps.connectionManager.GetConnections())

	// If no connections tracked but we had successful network connections,
	// count the network connections instead
	if connectionCount == 0 && result.SuccessCount > 0 {
		connectionCount = result.SuccessCount
	}

	ps.logger.LogToFrontend("INFO", "Discovery completed - found %d connections", connectionCount)

	// Start background discovery process
	ps.peerManager.StartBackgroundDiscovery()

	return &PeerDiscoveryResult{
		Connected:    connectionCount > 0,
		Peers:        connectionCount,
		HasPeers:     connectionCount > 0,
		SuccessCount: result.SuccessCount,
		FailureCount: result.FailureCount,
	}, nil
}

// GetStatus returns the current connection status
func (ps *ConcretePeerService) GetStatus() (bool, int, error) {
	connectionCount := len(ps.connectionManager.GetConnections())
	return connectionCount > 0, connectionCount, nil
}

// Close shuts down the P2P network
func (ps *ConcretePeerService) Close() error {
	ps.logger.LogToFrontend("INFO", "Closing P2P network...")
	ctx := context.Background()

	// Publish peer leave message before removing the connection
	if topicHandle != nil {
		leaveMessage := ps.connectionManager.createPeerStatusMessage(ps.hostData.ID(), "LEFT")
		bytes := []byte(leaveMessage)
		err := topicHandle.Publish(ctx, bytes)
		if err != nil {
			ps.logger.LogToFrontend("ERROR", "Failed to publish leave message: %v", err)
		}
	}

	// Close all connections in the connection manager
	connections := ps.connectionManager.GetConnections()
	for _, conn := range connections {
		ps.connectionManager.removeConnection(conn)
	}

	// Clean up DHT advertisements
	if dht := ps.peerManager.GetDHT(); dht != nil {
		ps.peerManager.cleanupDHT(dht, ps.logger)
	}

	// Close the host to disconnect from relay
	if ps.hostData != nil {
		ps.logger.LogToFrontend("INFO", "Disconnecting from relay...")
		if err := ps.hostData.Close(); err != nil {
			return err
		}
	}

	// Signal done
	ps.peerManager.done <- true
	return nil
}

// GetLogChannel returns the log channel for receiving log messages
func (ps *ConcretePeerService) GetLogChannel() <-chan LogMessage {
	return ps.logger.GetLogChannel()
}
