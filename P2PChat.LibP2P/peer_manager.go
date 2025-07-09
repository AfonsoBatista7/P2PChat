package main

import (
	"context"
	"sync"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
)

// PeerManager struct to manage peer state
type PeerManager struct {
	done        chan bool // Channel to signal termination
	disconnect  chan bool // Channel to signal termination
	unsubscribe chan bool // Channel to signal termination
}

var topicHandle *pubsub.Topic

func (p *PeerManager) StartProtocolP2P(cBootstrapPeers []string, debug bool, playerId string, hostData host.Host, logger *Logger, connectionManager *ConnectionManager) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up DHT
	kademliaDht, err := dht.New(ctx, hostData)
	if err != nil {
		logger.LogToFrontend("ERROR", "Failed to create DHT: %s", err)
		return
	}

	if err = kademliaDht.Bootstrap(ctx); err != nil {
		logger.LogToFrontend("ERROR", "Failed to bootstrap the DHT: %s", err)
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

	// Set up GossipSub
	gossipSub, err := pubsub.NewGossipSub(ctx, hostData)
	if err != nil {
		logger.LogToFrontend("ERROR", "Failed to create GossipSub: %s", err)
		return
	}

	go p.discover(ctx, hostData, kademliaDht, logger, connectionManager)

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
	discovery := routing.NewRoutingDiscovery(kademliaDht)

	// Advertise our presence
	util.Advertise(ctx, discovery, RendezvousString)

	// Use a more reasonable ticker interval (5 seconds)
	ticker := time.NewTicker(ConnectionTimeout)

	// Track the previous list of peers
	var previousPeers []peer.AddrInfo
	previousPeersMutex := &sync.RWMutex{}

	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			kademliaDht.RefreshRoutingTable()
			peers, err := util.FindPeers(ctx, discovery, RendezvousString)
			if err != nil {
				logger.LogToFrontend("ERROR", "Error finding peers: %v", err)
				continue
			}

			// Get the current list of peers
			currentPeers := peers

			// Compare with previous list to find new peers
			previousPeersMutex.RLock()
			newPeers := findNewPeers(currentPeers, previousPeers)
			previousPeersMutex.RUnlock()

			// Update previous peers list
			previousPeersMutex.Lock()
			previousPeers = currentPeers
			previousPeersMutex.Unlock()

			// Only try to connect to new peers
			for _, peer := range newPeers {
				if peer.ID == host.ID() {
					continue
				}

				// Check if we've failed to connect too many times
				connectionManager.failedConnectionsMutex.RLock()
				failCount := connectionManager.failedConnections[peer.ID]
				connectionManager.failedConnectionsMutex.RUnlock()

				if failCount >= 2 { // Skip if we've failed 2 or more times
					continue
				}

				// Update peerstore with peer addresses
				host.Peerstore().AddAddrs(peer.ID, peer.Addrs, peerstore.TempAddrTTL)

				connectedness := host.Network().Connectedness(peer.ID)

				if connectedness != network.Connected {
					_, err := host.Network().DialPeer(ctx, peer.ID)
					if err != nil {
						connectionManager.failedConnectionsMutex.Lock()
						connectionManager.failedConnections[peer.ID]++
						connectionManager.failedConnectionsMutex.Unlock()
						continue
					}

					logger.LogToFrontend("INFO", "Connected to peer %s", peer.ID.String())
				}
			}
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

// Helper function to find peers that are in currentPeers but not in previousPeers
func findNewPeers(currentPeers, previousPeers []peer.AddrInfo) []peer.AddrInfo {
	previousPeerMap := make(map[peer.ID]struct{})
	for _, p := range previousPeers {
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
