package main

import (
	"context"
	"sync"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
)

// DiscoveryConfig holds configuration for peer discovery
type DiscoveryConfig struct {
	RendezvousString  string
	ConnectionTimeout time.Duration
	AdvertiseInterval time.Duration
	MaxConcurrent     int
	ValidationTimeout time.Duration
}

// PeerDiscoveryManager handles all peer discovery operations
type PeerDiscoveryManager struct {
	config            DiscoveryConfig
	attemptedPeers    map[peer.ID]struct{}
	attemptedMutex    *sync.RWMutex
	previousPeers     []peer.AddrInfo
	previousMutex     *sync.RWMutex
	connectionManager *ConnectionManager
	logger            *Logger
}

// NewPeerDiscoveryManager creates a new peer discovery manager
func NewPeerDiscoveryManager(config DiscoveryConfig, connectionManager *ConnectionManager, logger *Logger) *PeerDiscoveryManager {
	return &PeerDiscoveryManager{
		config:            config,
		attemptedPeers:    make(map[peer.ID]struct{}),
		attemptedMutex:    &sync.RWMutex{},
		previousPeers:     make([]peer.AddrInfo, 0),
		previousMutex:     &sync.RWMutex{},
		connectionManager: connectionManager,
		logger:            logger,
	}
}

// DiscoveryResult holds the result of a discovery operation
type DiscoveryResult struct {
	Peers        []peer.AddrInfo
	ValidPeers   []peer.AddrInfo
	NewPeers     []peer.AddrInfo
	SuccessCount int
	FailureCount int
}

// DiscoverPeers performs peer discovery with validation and connection
func (pdm *PeerDiscoveryManager) DiscoverPeers(ctx context.Context, host host.Host, dht *dht.IpfsDHT) *DiscoveryResult {
	discovery := routing.NewRoutingDiscovery(dht)

	// Refresh routing table and find peers
	dht.RefreshRoutingTable()
	peers, err := util.FindPeers(ctx, discovery, pdm.config.RendezvousString)
	if err != nil {
		pdm.logger.LogToFrontend("ERROR", "Error finding peers in attempt: %v", err)
		return &DiscoveryResult{}
	}

	// Find truly new peers that aren't in our expired/attempted list
	newPeers := pdm.findTrulyNewPeers(peers)

	// Validate peers to filter out stale entries
	validPeers := pdm.validatePeers(ctx, host, newPeers)

	// Connect to peers in parallel
	successCount, failureCount := pdm.connectToPeersParallel(ctx, host, validPeers)

	return &DiscoveryResult{
		Peers:        peers,
		ValidPeers:   validPeers,
		NewPeers:     validPeers,
		SuccessCount: successCount,
		FailureCount: failureCount,
	}
}

// validatePeers filters out stale peers that are no longer reachable
func (pdm *PeerDiscoveryManager) validatePeers(ctx context.Context, host host.Host, peers []peer.AddrInfo) []peer.AddrInfo {
	var validPeers []peer.AddrInfo

	for _, peer := range peers {
		// Skip ourselves
		if peer.ID == host.ID() {
			continue
		}

		// Check if we're already connected to this peer
		if host.Network().Connectedness(peer.ID) == network.Connected {
			validPeers = append(validPeers, peer)
			continue
		}

		// Try to validate the peer by checking if it's reachable
		if len(peer.Addrs) > 0 {
			host.Peerstore().AddAddrs(peer.ID, peer.Addrs, peerstore.TempAddrTTL)

			testCtx, cancel := context.WithTimeout(ctx, pdm.config.ValidationTimeout)
			_, err := host.Network().DialPeer(testCtx, peer.ID)
			cancel()

			if err == nil {
				validPeers = append(validPeers, peer)
			} else {
				pdm.markPeerAsFailed(peer.ID)
			}
		}
	}

	return validPeers
}

// findTrulyNewPeers finds peers that are not in our expired/attempted list
func (pdm *PeerDiscoveryManager) findTrulyNewPeers(allPeers []peer.AddrInfo) []peer.AddrInfo {
	pdm.attemptedMutex.RLock()
	defer pdm.attemptedMutex.RUnlock()

	var trulyNewPeers []peer.AddrInfo
	for _, peer := range allPeers {
		// Only include peers that are NOT in our expired/attempted list
		if _, exists := pdm.attemptedPeers[peer.ID]; !exists {
			trulyNewPeers = append(trulyNewPeers, peer)
		}
	}
	return trulyNewPeers
}

// filterNewPeers filters out peers we've already attempted to connect to
func (pdm *PeerDiscoveryManager) filterNewPeers(allPeers []peer.AddrInfo) []peer.AddrInfo {
	pdm.attemptedMutex.RLock()
	defer pdm.attemptedMutex.RUnlock()

	var newPeers []peer.AddrInfo
	for _, peer := range allPeers {
		if _, exists := pdm.attemptedPeers[peer.ID]; !exists {
			newPeers = append(newPeers, peer)
		}
	}
	return newPeers
}

// connectToPeersParallel attempts to connect to peers in parallel
func (pdm *PeerDiscoveryManager) connectToPeersParallel(ctx context.Context, host host.Host, peers []peer.AddrInfo) (int, int) {
	if len(peers) == 0 {
		return 0, 0
	}

	if len(peers) > 0 {
		pdm.logger.LogToFrontend("INFO", "Connecting to %d peers", len(peers))
	}

	// Use semaphore to limit concurrent connections
	semaphore := make(chan struct{}, pdm.config.MaxConcurrent)

	var wg sync.WaitGroup
	successCount := 0
	failureCount := 0
	var resultMutex sync.Mutex

	for _, peer := range peers {
		if peer.ID == host.ID() {
			continue
		}

		// Mark this peer as attempted
		pdm.markPeerAsAttempted(peer.ID)

		// Skip if we've failed too many times
		if pdm.getFailedCount(peer.ID) >= 2 {
			continue
		}

		wg.Add(1)
		go func() {
			// Capture the peer variable in the closure
			peerToConnect := peer
			defer wg.Done()

			// Acquire semaphore slot
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Update peerstore with peer addresses
			host.Peerstore().AddAddrs(peerToConnect.ID, peerToConnect.Addrs, peerstore.TempAddrTTL)

			connectedness := host.Network().Connectedness(peerToConnect.ID)
			if connectedness != network.Connected {
				// Use timeout for each individual connection attempt
				connectCtx, cancel := context.WithTimeout(ctx, pdm.config.ConnectionTimeout)
				defer cancel()

				_, err := host.Network().DialPeer(connectCtx, peerToConnect.ID)
				if err != nil {
					pdm.markPeerAsFailed(peerToConnect.ID)

					resultMutex.Lock()
					failureCount++
					resultMutex.Unlock()

					pdm.logger.LogToFrontend("ERROR", "Failed: %s - %v", peerToConnect.ID, err)
					return
				}

				resultMutex.Lock()
				successCount++
				resultMutex.Unlock()

				pdm.logger.LogToFrontend("INFO", "Connected: %s", peerToConnect.ID.String())
			}
		}()
	}

	// Wait for all connection attempts to complete
	wg.Wait()

	if successCount > 0 || failureCount > 0 {
		pdm.logger.LogToFrontend("INFO", "%d connected, %d failed", successCount, failureCount)
	}
	return successCount, failureCount
}

// markPeerAsAttempted marks a peer as attempted
func (pdm *PeerDiscoveryManager) markPeerAsAttempted(peerID peer.ID) {
	pdm.attemptedMutex.Lock()
	pdm.attemptedPeers[peerID] = struct{}{}
	pdm.attemptedMutex.Unlock()
}

// markPeerAsFailed marks a peer as failed
func (pdm *PeerDiscoveryManager) markPeerAsFailed(peerID peer.ID) {
	if pdm.connectionManager != nil {
		pdm.connectionManager.AddFailedConnection(peerID)
	}
	pdm.markPeerAsAttempted(peerID)
}

// getFailedCount gets the failed connection count for a peer
func (pdm *PeerDiscoveryManager) getFailedCount(peerID peer.ID) int {
	if pdm.connectionManager != nil {
		return pdm.connectionManager.GetFailedConnectionCount(peerID)
	}
	return 0
}

// UpdatePreviousPeers updates the list of previous peers
func (pdm *PeerDiscoveryManager) UpdatePreviousPeers(peers []peer.AddrInfo) {
	pdm.previousMutex.Lock()
	pdm.previousPeers = peers
	pdm.previousMutex.Unlock()
}

// FindNewPeers finds peers that are in currentPeers but not in previousPeers
func (pdm *PeerDiscoveryManager) FindNewPeers(currentPeers []peer.AddrInfo) []peer.AddrInfo {
	pdm.previousMutex.RLock()
	defer pdm.previousMutex.RUnlock()

	previousPeerMap := make(map[peer.ID]struct{})
	for _, p := range pdm.previousPeers {
		previousPeerMap[p.ID] = struct{}{}
	}

	var newPeers []peer.AddrInfo
	for _, p := range currentPeers {
		if _, exists := previousPeerMap[p.ID]; !exists {
			newPeers = append(newPeers, p)
		}
	}
	return newPeers
}
