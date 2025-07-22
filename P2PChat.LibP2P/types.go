package main

import (
	"bufio"
	"fmt"
	"io"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	quic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/multiformats/go-multiaddr"
)

// Constants
const (
	RendezvousString  = "METAVERSE"
	ProtocolID        = "/metaverse/1.0.0"
	TopicName         = "chat"
	ConnectionTimeout = 30 * time.Second // Increased from 5 seconds to 30 seconds
	AdvertiseInterval = 2 * time.Minute  // How often to re-advertise ourselves
	LogChannelBuffer  = 100
	LogTimeout        = 1 * time.Second
)

// Connection represents a peer connection
type Connection struct {
	RW       *bufio.ReadWriter
	PeerID   peer.ID
	LastSeen time.Time
}

// APIRequest represents incoming API requests
type APIRequest struct {
	PeerID    string `json:"peerId"`
	Message   string `json:"message"`
	Bootstrap string `json:"bootstrap"`
	Debug     bool   `json:"debug"`
}

// APIResponse represents API responses
type APIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

// LogMessage represents log messages sent to frontend
type LogMessage struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// MakeHost creates a new libp2p host with default settings
func MakeHost(randomness io.Reader) (host.Host, error) {
	prvKey, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, randomness)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	sourceMultiAddrUDP, _ := multiaddr.NewMultiaddr("/ip4/0.0.0.0/udp/0/quic-v1")

	return libp2p.New(
		libp2p.ListenAddrs(sourceMultiAddrUDP),
		libp2p.Transport(quic.NewTransport),
		libp2p.Identity(prvKey),
		libp2p.NATPortMap(),
		libp2p.EnableHolePunching(),
	)
}
