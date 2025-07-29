package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
	quic "github.com/libp2p/go-libp2p/p2p/transport/quic"

	"github.com/multiformats/go-multiaddr"
)

var (
	readWriter  []*bufio.ReadWriter
	hostData    host.Host
	contextVar  context.Context
	kademliaDht *dht.IpfsDHT
	discovery   *routing.RoutingDiscovery
	gossipSub   *pubsub.PubSub
	subscriber  *pubsub.Subscription
	topicHandle *pubsub.Topic
	pm          *PeerManager // Add PeerManager to global variables
)

var rendezvousString = "METAVERSE"

type connection struct {
	rw       *bufio.ReadWriter
	peerID   peer.ID
	lastSeen time.Time
}

var connections []*connection
var connectionsMutex sync.RWMutex
var failedConnections = make(map[peer.ID]int) // Track failed connection attempts
var failedConnectionsMutex sync.RWMutex

type APIRequest struct {
	PeerID    string `json:"peerId"`
	Message   string `json:"message"`
	Bootstrap string `json:"bootstrap"`
	Debug     bool   `json:"debug"`
}

type APIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

type LogMessage struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

var logChannel = make(chan LogMessage, 100) // Buffer for logs

func logToFrontend(level string, format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	logMsg := LogMessage{Level: level, Message: message}

	// Try to send to channel with timeout
	select {
	case logChannel <- logMsg:
		// Successfully sent to channel
	case <-time.After(1 * time.Second):
		// Channel is full or blocked, log to console
		log.Printf("[%s] Failed to send log to channel: %s", level, message)
	}

	// Only log to console if it's an error
	if level == "ERROR" {
		log.Printf("[%s] %s", level, message)
	}
}

// Will only be run on the receiving side.
func handleStream(s network.Stream) {
	logToFrontend("INFO", "Got a new stream!")
	logToFrontend("INFO", "Connected to peer!")

	// Create a buffer stream for non-blocking read and write.
	rw := bufio.NewReadWriter(bufio.NewReader(s), bufio.NewWriter(s))

	conn := &connection{
		rw:       rw,
		peerID:   s.Conn().RemotePeer(),
		lastSeen: time.Now(),
	}

	// Publish peer join message
	if topicHandle != nil {
		joinMessage := createPeerStatusMessage(conn.peerID, "JOINED")
		bytes := []byte(joinMessage)
		err := topicHandle.Publish(contextVar, bytes)
		if err != nil {
			logToFrontend("ERROR", "Failed to publish join message: %v", err)
		}
	}

	connectionsMutex.Lock()
	connections = append(connections, conn)
	connectionsMutex.Unlock()

	// Reset failed connection count for this peer
	failedConnectionsMutex.Lock()
	delete(failedConnections, s.Conn().RemotePeer())
	failedConnectionsMutex.Unlock()

	go readData(conn)
}

func readData(conn *connection) {
	logToFrontend("INFO", "Reading Data...")
	for {
		str, err := conn.rw.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				logToFrontend("ERROR", "Error reading from peer %s: %v", conn.peerID, err)
			}
			removeConnection(conn)
			return
		}

		if str == "" {
			return
		}

		if str != "\n" {
			logToFrontend("MSG", "Anon: %s", str)
		}

		// Update last seen time
		conn.lastSeen = time.Now()
	}
}

func writeData(sendData string) {
	for _, conn := range connections {
		if time.Since(conn.lastSeen) > 5*time.Minute {
			// Connection is stale, remove it
			removeConnection(conn)
			continue
		}

		_, err := conn.rw.WriteString(fmt.Sprintf("%s\n", sendData))
		if err != nil {
			logToFrontend("ERROR", "Error writing to peer %s: %v", conn.peerID, err)
			removeConnection(conn)
			continue
		}

		err = conn.rw.Flush()
		if err != nil {
			logToFrontend("ERROR", "Error flushing to peer %s: %v", conn.peerID, err)
			removeConnection(conn)
			continue
		}
	}
}

// Helper function to create a peer status message
func createPeerStatusMessage(peerID peer.ID, status string) string {
	return fmt.Sprintf("PEER_STATUS:%s:%s", peerID.String(), status)
}

func removeConnection(conn *connection) {
	// Publish peer leave message before removing the connection
	if topicHandle != nil {
		leaveMessage := createPeerStatusMessage(conn.peerID, "LEFT")
		bytes := []byte(leaveMessage)
		err := topicHandle.Publish(contextVar, bytes)
		if err != nil {
			logToFrontend("ERROR", "Failed to publish leave message: %v", err)
		}
	}

	// Remove the connection from the slice
	for i, c := range connections {
		if c == conn {
			connections = append(connections[:i], connections[i+1:]...)
			break
		}
	}
}

func connectBootstrapPeer(ctx context.Context, host host.Host, peerinfo peer.AddrInfo, wg *sync.WaitGroup) {
	wg.Add(1)

	go func() {
		defer wg.Done()

		// First add the peer's addresses to the peerstore
		host.Peerstore().AddAddrs(peerinfo.ID, peerinfo.Addrs, peerstore.PermanentAddrTTL)

		err := host.Connect(ctx, peerinfo)
		if err != nil {
			logToFrontend("ERROR", "Failed to connect to peer %s: %s", peerinfo.ID, err.Error())
			return
		}

		logToFrontend("INFO", "[RELAY CONNECTED]")
	}()
}

func (p *PeerManager) startProtocolP2P(cBootstrapPeers []string, debug bool, playerId string) {
	readWriter = make([]*bufio.ReadWriter, 0, 5)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	contextVar = ctx

	var r io.Reader = rand.Reader

	err := error(nil)

	hostData, err = makeHost(r)
	if err != nil {
		logToFrontend("ERROR", "Failed to create host: %s", err)
		return
	}

	logToFrontend("INFO", "My peer ID -> %s", hostData.ID())

	if debug {
		logToFrontend("INFO", "Debug mode: %t", debug)
	}

	hostData.SetStreamHandler("/metaverse/1.0.0", handleStream)

	// Create a new KadDHT instance and connect to the bootstrap nodes to populate the routing table
	createKadAndConnectToRelays(ctx, hostData, debug, cBootstrapPeers)

	// create a new PubSub service using the GossipSub router
	startGossipSub(ctx, hostData)

	go p.discover(ctx, hostData, playerId)

	joinTopic("iot")

	//send Notification that peer started
	logToFrontend("INFO", "Peer started successfully!")

	// Wait until the peer is terminated
	<-p.done
	logToFrontend("INFO", "Closing peer...")
}

func createKadAndConnectToRelays(ctx context.Context, host host.Host, debug bool, cBootstrapPeers []string) {
	err := error(nil)

	kademliaDht, err = dht.New(ctx, host)
	if err != nil {
		logToFrontend("ERROR", "Failed to create DHT: %s", err)
		return
	}

	// Bootstrap the DHT
	if err = kademliaDht.Bootstrap(ctx); err != nil {
		logToFrontend("ERROR", "Failed to bootstrap the DHT: %s", err)
	}

	var wg sync.WaitGroup

	if debug {
		logToFrontend("INFO", "Using default bootstrap peers")
		for _, addr := range dht.DefaultBootstrapPeers {
			peerinfo, err := peer.AddrInfoFromP2pAddr(addr)
			if err != nil {
				logToFrontend("ERROR", "Failed to parse bootstrap peer address %s: %s", addr, err)
				continue
			}
			connectBootstrapPeer(ctx, host, *peerinfo, &wg)
		}
	} else {
		if len(cBootstrapPeers) == 0 {
			logToFrontend("ERROR", "No bootstrap peers provided")
			return
		}
		//logToFrontend("INFO", "Using custom bootstrap peers: %v", cBootstrapPeers)
		for _, addr := range cBootstrapPeers {
			peerinfo, err := peer.AddrInfoFromString(addr)
			if err != nil {
				logToFrontend("ERROR", "Failed to parse bootstrap peer address %s: %s", addr, err)
				continue
			}
			connectBootstrapPeer(ctx, host, *peerinfo, &wg)
		}
	}

	// Wait for all connection attempts to complete
	wg.Wait()
}

func startGossipSub(ctx context.Context, host host.Host) {
	err := error(nil)

	gossipSub, err = pubsub.NewGossipSub(ctx, host)
	if err != nil {
		logToFrontend("ERROR", "Failed to create GossipSub: %s", err)
	}
}

func (p *PeerManager) discover(ctx context.Context, host host.Host, playerId string) {
	discovery = routing.NewRoutingDiscovery(kademliaDht)

	// Advertise our presence
	util.Advertise(ctx, discovery, rendezvousString)

	// Use a more reasonable ticker interval (5 seconds)
	ticker := time.NewTicker(5 * time.Second)

	// Track the previous list of peers
	var previousPeers []peer.AddrInfo
	previousPeersMutex := &sync.RWMutex{}

	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			kademliaDht.RefreshRoutingTable()
			peers, err := util.FindPeers(ctx, discovery, rendezvousString)
			if err != nil {
				logToFrontend("ERROR", "Error finding peers: %v", err)
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
				failedConnectionsMutex.RLock()
				failCount := failedConnections[peer.ID]
				failedConnectionsMutex.RUnlock()

				if failCount >= 2 { // Skip if we've failed 2 or more times
					continue
				}

				// Update peerstore with peer addresses
				host.Peerstore().AddAddrs(peer.ID, peer.Addrs, peerstore.TempAddrTTL)

				connectedness := host.Network().Connectedness(peer.ID)

				if connectedness != network.Connected {
					_, err := host.Network().DialPeer(ctx, peer.ID)
					if err != nil {
						failedConnectionsMutex.Lock()
						failedConnections[peer.ID]++
						failedConnectionsMutex.Unlock()
						continue
					}

					logToFrontend("INFO", "Connected to peer %s", peer.ID.String())
				}
			}
		}
	}
}

// Helper function to find peers that are in currentPeers but not in previousPeers
func findNewPeers(currentPeers, previousPeers []peer.AddrInfo) []peer.AddrInfo {
	// Create a map of previous peer IDs for quick lookup
	previousPeerMap := make(map[peer.ID]struct{})
	for _, p := range previousPeers {
		previousPeerMap[p.ID] = struct{}{}
	}

	// Find peers that aren't in the previous list
	var newPeers []peer.AddrInfo
	for _, p := range currentPeers {
		if _, exists := previousPeerMap[p.ID]; !exists {
			newPeers = append(newPeers, p)
		}
	}

	return newPeers
}

// publish to topic
func publish(stateData string) {
	if len(stateData) == 0 {
		return
	}

	// Publish message to topic
	bytes := []byte(stateData)
	err := topicHandle.Publish(contextVar, bytes)
	if err != nil {
		logToFrontend("ERROR", "Failed to publish message: %v", err)
		return
	}
}

// join the pubsub topic and start subscribing to data
func joinTopic(topic string) {
	err := error(nil)

	topicHandle, err = gossipSub.Join(topic)

	if err != nil {
		logToFrontend("ERROR", "Failed to join topic: %s", err)
		return
	}

	// subscribe to topic
	subscriber, err = topicHandle.Subscribe()
	if err != nil {
		logToFrontend("ERROR", "Failed to subscribe to topic: %s", err.Error())
		return
	}

	go subscribe(subscriber, contextVar, hostData.ID())
}

// start subsriber to topic
func subscribe(subscriber *pubsub.Subscription, ctx context.Context, hostID peer.ID) {
	for {
		msg, err := subscriber.Next(ctx)
		if err != nil {
			logToFrontend("ERROR", "Error subscribing to topic: %s", err.Error())
			return
		}

		// only consider messages delivered by other peers
		if msg.ReceivedFrom == hostID {
			continue
		}

		logToFrontend("MSG", "Anon: %s", string(msg.Data))
	}
}

func makeHost(randomness io.Reader) (host.Host, error) {
	// Creates a new RSA key pair for this host.
	prvKey, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, randomness)
	if err != nil {
		logToFrontend("ERROR", "Failed to generate private key: %s", err)
		return nil, err
	}

	// 0.0.0.0 will listen on any interface device.
	sourceMultiAddrUDP, _ := multiaddr.NewMultiaddr("/ip4/0.0.0.0/udp/0/quic-v1")

	// libp2p.New constructs a new libp2p Host.
	// Other options can be added here.
	return libp2p.New(
		libp2p.ListenAddrs(sourceMultiAddrUDP),
		libp2p.Transport(quic.NewTransport),
		libp2p.Identity(prvKey),

		// Attempt to open ports using uPNP for NATed hosts.
		libp2p.NATPortMap(),
		libp2p.EnableHolePunching(),
	)
}

func connectToPeer(peerID string) {
	kademliaDht.RefreshRoutingTable()

	peerIdObj, err := findPeer(peerID)
	if err != nil {
		logToFrontend("ERROR", "%s", err)
		return
	}

	conn, err := connectToPeerAction(peerIdObj)
	if err != nil {
		logToFrontend("ERROR", "Failed to start protocol: %s", err)
		return
	}

	go readData(conn)
}

func findPeer(peerID string) (peer.ID, error) {
	var foundPeers []peer.AddrInfo

	logToFrontend("INFO", "Searching for peer...")

	peerChan, err := util.FindPeers(contextVar, discovery, peerID)
	if err != nil {
		logToFrontend("ERROR", "Failed to find peer: %s", err)
	}

	for i := 0; i < len(peerChan); i++ {
		if peerChan[i].ID.String() == "" {
			continue
		}

		logToFrontend("INFO", "Peer found: %s", peerChan[i].ID.String())
		foundPeers = append(foundPeers, peerChan[i])

		// Add the peer address to the peerstore
		hostData.Peerstore().AddAddrs(peerChan[i].ID, peerChan[i].Addrs, peerstore.PermanentAddrTTL)

	}

	if len(foundPeers) == 0 {
		return "", errors.New("peer not found")
	}

	for _, peer := range foundPeers {
		if !strings.HasPrefix(peer.ID.String(), "12D3") {
			return peer.ID, nil
		}
	}

	return "", errors.New("no valid peers found")
}

func connectToPeerAction(peerID peer.ID) (*connection, error) {
	// Start a stream with the destination.
	// Multiaddress of the destination peer is fetched from the peerstore using 'peerId'.
	s, err := hostData.NewStream(context.Background(), peerID, "/metaverse/1.0.0")
	if err != nil {
		logToFrontend("ERROR", "Failed to create new stream: %s", err)
		return nil, err
	}
	logToFrontend("INFO", "Established connection to destination")

	// Create a buffered stream so that read and writes are non-blocking.
	rw := bufio.NewReadWriter(bufio.NewReader(s), bufio.NewWriter(s))

	conn := &connection{
		rw:       rw,
		peerID:   peerID,
		lastSeen: time.Now(),
	}

	connections = append(connections, conn)

	return conn, nil
}

func (p *PeerManager) unsubscribeTopic() {
	subscriber.Cancel()
	topicHandle.Close()
	p.unsubscribe <- true
}

func (p *PeerManager) closePeer() {
	if hostData != nil {
		kademliaDht.Close()
		hostData.Close()
	} else {
		logToFrontend("ERROR", "hostData is nil in closePeer")
	}
}

func startHTTPServer(port string) {
	// Set up all handlers first
	http.HandleFunc("/api/start", handleStart)
	http.HandleFunc("/api/connect", handleConnect)
	http.HandleFunc("/api/send", handleSend)
	http.HandleFunc("/api/close", handleClose)
	http.HandleFunc("/api/status", handleStatus)
	http.HandleFunc("/api/logs", handleLogs)

	// Start the server in a goroutine to allow for graceful shutdown
	server := &http.Server{
		Addr: ":" + port,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logToFrontend("ERROR", "HTTP server failed: %v", err)
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()
}

func handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req APIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logToFrontend("ERROR", "Invalid request body: %v", err)
		sendJSONResponse(w, APIResponse{Success: false, Error: "Invalid request body"})
		return
	}

	// Start P2P with the provided parameters
	go func() {
		pm = &PeerManager{
			done: make(chan bool),
		}
		pm.startProtocolP2P([]string{req.Bootstrap}, req.Debug, req.PeerID)
	}()

	sendJSONResponse(w, APIResponse{Success: true, Message: "P2P network started"})
}

func handleClose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	publish(fmt.Sprintf("%s has left the chat", hostData.ID()))

	pm.closePeer() // Run in background
	sendJSONResponse(w, APIResponse{Success: true, Message: "Peer connection closed"})

	pm.done <- true
}

func handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req APIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSONResponse(w, APIResponse{Success: false, Error: "Invalid request body"})
		return
	}

	if req.PeerID == "" {
		sendJSONResponse(w, APIResponse{Success: false, Error: "PeerID is required"})
		return
	}

	connectToPeer(req.PeerID)
	sendJSONResponse(w, APIResponse{Success: true, Message: "Connection attempt initiated"})
}

func handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req APIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSONResponse(w, APIResponse{Success: false, Error: "Invalid request body"})
		return
	}

	if req.Message == "" {
		sendJSONResponse(w, APIResponse{Success: false, Error: "Message is required"})
		return
	}

	publish(req.Message)
	sendJSONResponse(w, APIResponse{Success: true, Message: "Message sent"})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := map[string]interface{}{
		"connected": len(connections) > 0,
		"peers":     len(connections),
		"started":   hostData != nil,
	}

	sendJSONResponse(w, APIResponse{Success: true, Message: "Status retrieved", Error: fmt.Sprintf("%v", status)})
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Set up Server-Sent Events
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Send logs as they come in
	for {
		select {
		case logMsg := <-logChannel:
			data, err := json.Marshal(logMsg)
			if err != nil {
				logToFrontend("ERROR", "Failed to marshal log message: %v", err)
				continue
			}
			_, err = fmt.Fprintf(w, "data: %s\n\n", data)
			if err != nil {
				logToFrontend("ERROR", "Failed to write log message: %v", err)
				return
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
}

func sendJSONResponse(w http.ResponseWriter, response APIResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func main() {
	// Parse command line arguments
	port := flag.String("port", "8080", "Port to listen on")
	flag.Parse()

	// Use environment variable PORT if set, otherwise use command line argument
	if envPort := os.Getenv("PORT"); envPort != "" {
		port = &envPort
	}

	// Start HTTP server in a separate goroutine
	go startHTTPServer(*port)

	// Keep the main goroutine alive
	select {}
}
