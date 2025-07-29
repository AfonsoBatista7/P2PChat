package main

import (
	"context"
	"sync"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
)

// PeerManager struct to manage peer state
type PeerManager struct {
	done              chan bool          // Channel to signal termination
	disconnect        chan bool          // Channel to signal termination
	unsubscribe       chan bool          // Channel to signal termination
	dht               *dht.IpfsDHT       // Store DHT reference for cleanup
	dhtReady          chan bool          // Channel to signal DHT is ready
	hostData          host.Host          // Store host reference for background discovery
	kademliaDht       *dht.IpfsDHT       // Store DHT reference for background discovery
	logger            *Logger            // Store logger reference for background discovery
	connectionManager *ConnectionManager // Store connection manager reference for background discovery
}

var topicHandle *pubsub.Topic

// initializeDHT sets up the DHT and connects to bootstrap peers
func (p *PeerManager) initializeDHT(ctx context.Context, hostData host.Host, cBootstrapPeers []string, debug bool, logger *Logger) (*dht.IpfsDHT, error) {
	// Set up DHT
	kademliaDht, err := dht.New(ctx, hostData)
	if err != nil {
		logger.LogToFrontend("ERROR", "Failed to create DHT: %s", err)
		return nil, err
	}

	if err = kademliaDht.Bootstrap(ctx); err != nil {
		logger.LogToFrontend("ERROR", "Failed to bootstrap the DHT: %s", err)
		return nil, err
	}

	var wg sync.WaitGroup

	if debug {
		logger.LogToFrontend("INFO", "Using default bootstrap peers")
		for _, addr := range dht.DefaultBootstrapPeers {
			peerinfo, err := peer.AddrInfoFromP2pAddr(addr)
			if err != nil {
				logger.LogToFrontend("ERROR", "Failed to parse bootstrap peer address %s: %s", addr, err)
				continue
			}
			p.connectBootstrapPeer(ctx, hostData, *peerinfo, &wg, logger)
		}
	} else {
		for _, addr := range cBootstrapPeers {
			peerinfo, err := peer.AddrInfoFromString(addr)
			if err != nil {
				logger.LogToFrontend("ERROR", "Failed to parse bootstrap peer address %s: %s", addr, err)
				continue
			}
			p.connectBootstrapPeer(ctx, hostData, *peerinfo, &wg, logger)
		}
	}
	wg.Wait()

	return kademliaDht, nil
}

func (p *PeerManager) StartProtocolP2P(cBootstrapPeers []string, debug bool, playerId string, hostData host.Host, logger *Logger, connectionManager *ConnectionManager) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize DHT
	kademliaDht, err := p.initializeDHT(ctx, hostData, cBootstrapPeers, debug, logger)
	if err != nil {
		return
	}

	// Store DHT reference for cleanup
	p.dht = kademliaDht

	// Signal that DHT is ready
	close(p.dhtReady)

	// Set up GossipSub
	gossipSub, err := pubsub.NewGossipSub(ctx, hostData)
	if err != nil {
		logger.LogToFrontend("ERROR", "Failed to create GossipSub: %s", err)
		return
	}

	// Store references for background discovery
	p.hostData = hostData
	p.kademliaDht = kademliaDht
	p.logger = logger
	p.connectionManager = connectionManager

	// Join topic and subscribe
	joinTopic(TopicName, gossipSub, hostData, ctx, p, logger)

	logger.LogToFrontend("INFO", "Peer started successfully!")
	<-p.done
	logger.LogToFrontend("INFO", "Closing peer...")
}

// join the pubsub topic and start subscribing to data
func joinTopic(topic string, gossipSub *pubsub.PubSub, hostData host.Host, ctx context.Context, p *PeerManager, logger *Logger) {
	err := error(nil)

	topicHandle, err = gossipSub.Join(topic)

	if err != nil {
		logger.LogToFrontend("ERROR", "Failed to join topic: %s", err)
		return
	}

	// subscribe to topic
	subscriber, err := topicHandle.Subscribe()
	if err != nil {
		logger.LogToFrontend("ERROR", "Failed to subscribe to topic: %s", err.Error())
		return
	}

	go p.subscribe(subscriber, ctx, hostData.ID(), logger)
}

func (p *PeerManager) publishMessage(ctx context.Context, message string, logger *Logger) {
	if len(message) == 0 {
		return
	}

	// Publish message to topic
	bytes := []byte(message)
	err := topicHandle.Publish(ctx, bytes)
	if err != nil {
		logger.LogToFrontend("ERROR", "Failed to publish message: %v", err)
		return
	}
}

func (p *PeerManager) discover(ctx context.Context, host host.Host, kademliaDht *dht.IpfsDHT, logger *Logger, connectionManager *ConnectionManager) {
	// Create discovery configuration
	config := DiscoveryConfig{
		RendezvousString:  RendezvousString,
		ConnectionTimeout: ConnectionTimeout,
		AdvertiseInterval: AdvertiseInterval,
		ValidationTimeout: 1 * time.Second,
		MaxConcurrent:     5,
	}

	// Create discovery manager
	discoveryManager := NewPeerDiscoveryManager(config, connectionManager, logger)

	// Advertise our presence
	discoveryInterface := routing.NewRoutingDiscovery(kademliaDht)
	util.Advertise(ctx, discoveryInterface, RendezvousString, discovery.TTL(AdvertisementTTL))

	// Give other peers a moment to advertise themselves
	time.Sleep(500 * time.Millisecond)

	// Perform initial discovery
	initialResult := discoveryManager.DiscoverPeers(ctx, host, kademliaDht)
	discoveryManager.UpdatePreviousPeers(initialResult.Peers)

	// Set up tickers
	ticker := time.NewTicker(ConnectionTimeout)
	advertiseTicker := time.NewTicker(AdvertiseInterval)

	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			// Perform periodic discovery (this already handles filtering and connection)
			discoveryManager.DiscoverPeers(ctx, host, kademliaDht)
		case <-advertiseTicker.C:
			// Re-advertise ourselves to stay discoverable
			util.Advertise(ctx, discoveryInterface, RendezvousString, discovery.TTL(AdvertisementTTL))
		}
	}
}

func (p *PeerManager) connectBootstrapPeer(ctx context.Context, host host.Host, peerinfo peer.AddrInfo, wg *sync.WaitGroup, logger *Logger) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		host.Peerstore().AddAddrs(peerinfo.ID, peerinfo.Addrs, peerstore.PermanentAddrTTL)
		err := host.Connect(ctx, peerinfo)
		if err != nil {
			logger.LogToFrontend("ERROR", "Failed to connect to peer %s: %s", peerinfo.ID, err.Error())
			return
		}
		logger.LogToFrontend("INFO", "[RELAY CONNECTED]")
	}()
}

func (p *PeerManager) subscribe(subscriber *pubsub.Subscription, ctx context.Context, hostID peer.ID, logger *Logger) {
	for {
		msg, err := subscriber.Next(ctx)
		if err != nil {
			logger.LogToFrontend("ERROR", "Error subscribing to topic: %s", err.Error())
			return
		}
		if msg.ReceivedFrom == hostID {
			continue
		}
		logger.LogToFrontend("MSG", "Anon: %s", string(msg.Data))
	}
}

// GetDHT returns the DHT reference for cleanup
func (p *PeerManager) GetDHT() *dht.IpfsDHT {
	return p.dht
}

// cleanupDHT removes our advertisement from the DHT when shutting down
func (p *PeerManager) cleanupDHT(kademliaDht *dht.IpfsDHT, logger *Logger) {
	logger.LogToFrontend("INFO", "Cleaning up DHT advertisements...")

	if kademliaDht != nil {
		// Try to actively remove our advertisement by advertising with a very short TTL
		// This helps ensure stale entries are cleaned up faster
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		discoveryInterface := routing.NewRoutingDiscovery(kademliaDht)
		// Advertise with minimal TTL (1 second) to effectively "remove" our presence
		util.Advertise(ctx, discoveryInterface, RendezvousString, discovery.TTL(1*time.Second))
		logger.LogToFrontend("INFO", "Sent cleanup advertisement with minimal TTL")

		// Wait a moment for the cleanup advertisement to propagate
		time.Sleep(2 * time.Second)

		// Close the DHT properly
		if err := kademliaDht.Close(); err != nil {
			logger.LogToFrontend("ERROR", "Error closing DHT: %v", err)
		} else {
			logger.LogToFrontend("INFO", "DHT closed successfully")
		}
	}
}

// WaitForDHTReady waits until DHT is initialized
func (p *PeerManager) WaitForDHTReady(timeout time.Duration) bool {
	select {
	case <-p.dhtReady:
		return true
	case <-time.After(timeout):
		return false
	}
}

// StartBackgroundDiscovery starts the background discovery process
func (p *PeerManager) StartBackgroundDiscovery() {
	if p.hostData == nil || p.kademliaDht == nil || p.logger == nil || p.connectionManager == nil {
		p.logger.LogToFrontend("ERROR", "Cannot start background discovery - required components not initialized")
		return
	}

	ctx := context.Background()
	p.logger.LogToFrontend("INFO", "Starting background discovery process...")
	go p.discover(ctx, p.hostData, p.kademliaDht, p.logger, p.connectionManager)
}
