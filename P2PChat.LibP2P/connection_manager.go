package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

// ConnectionManager handles all peer connections
type ConnectionManager struct {
	connections            []*Connection
	connectionsMutex       sync.RWMutex
	failedConnections      map[peer.ID]int
	failedConnectionsMutex sync.RWMutex
	logger                 *Logger
	host                   host.Host
	topicHandle            interface{} // Will be properly typed when pubsub is imported
	contextVar             context.Context
}

// NewConnectionManager creates a new connection manager
func NewConnectionManager(logger *Logger, host host.Host) *ConnectionManager {
	return &ConnectionManager{
		connections:       make([]*Connection, 0),
		failedConnections: make(map[peer.ID]int),
		logger:            logger,
		host:              host,
	}
}

// SetTopicHandle sets the topic handle for publishing peer status messages
func (cm *ConnectionManager) SetTopicHandle(topicHandle interface{}) {
	cm.topicHandle = topicHandle
}

// SetContext sets the context for operations
func (cm *ConnectionManager) SetContext(ctx context.Context) {
	cm.contextVar = ctx
}

// HandleStream handles incoming streams from peers
func (cm *ConnectionManager) HandleStream(s network.Stream) {
	cm.logger.LogToFrontend("INFO", "Got a new stream!")
	cm.logger.LogToFrontend("INFO", "Connected to peer!")

	// Create a buffer stream for non-blocking read and write.
	rw := bufio.NewReadWriter(bufio.NewReader(s), bufio.NewWriter(s))

	conn := &Connection{
		RW:       rw,
		PeerID:   s.Conn().RemotePeer(),
		LastSeen: time.Now(),
	}

	// Publish peer join message (if pubsub is integrated)
	if cm.topicHandle != nil {
		// joinMessage := cm.createPeerStatusMessage(conn.PeerID, "JOINED")
		// bytes := []byte(joinMessage)
		// err := cm.topicHandle.Publish(cm.contextVar, bytes)
		// if err != nil {
		// 	cm.logger.LogToFrontend("ERROR", "Failed to publish join message: %v", err)
		// }
	}

	cm.connectionsMutex.Lock()
	cm.connections = append(cm.connections, conn)
	cm.connectionsMutex.Unlock()

	// Reset failed connection count for this peer
	cm.failedConnectionsMutex.Lock()
	delete(cm.failedConnections, s.Conn().RemotePeer())
	cm.failedConnectionsMutex.Unlock()

	go cm.readData(conn)
}

// ReadData reads data from a connection
func (cm *ConnectionManager) readData(conn *Connection) {
	cm.logger.LogToFrontend("INFO", "Reading Data...")
	for {
		str, err := conn.RW.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				cm.logger.LogToFrontend("ERROR", "Error reading from peer %s: %v", conn.PeerID, err)
			}
			cm.removeConnection(conn)
			return
		}

		if str == "" {
			return
		}

		if str != "\n" {
			cm.logger.LogToFrontend("MSG", "Anon: %s", str)
		}

		// Update last seen time
		conn.LastSeen = time.Now()
	}
}

// WriteData writes data to all connections
func (cm *ConnectionManager) WriteData(sendData string) {
	cm.connectionsMutex.RLock()
	connections := make([]*Connection, len(cm.connections))
	copy(connections, cm.connections)
	cm.connectionsMutex.RUnlock()

	for _, conn := range connections {
		if time.Since(conn.LastSeen) > ConnectionTimeout {
			// Connection is stale, remove it
			cm.removeConnection(conn)
			continue
		}

		_, err := conn.RW.WriteString(fmt.Sprintf("%s\n", sendData))
		if err != nil {
			cm.logger.LogToFrontend("ERROR", "Error writing to peer %s: %v", conn.PeerID, err)
			cm.removeConnection(conn)
			continue
		}

		err = conn.RW.Flush()
		if err != nil {
			cm.logger.LogToFrontend("ERROR", "Error flushing to peer %s: %v", conn.PeerID, err)
			cm.removeConnection(conn)
			continue
		}
	}
}

// CreatePeerStatusMessage creates a peer status message
func (cm *ConnectionManager) createPeerStatusMessage(peerID peer.ID, status string) string {
	return fmt.Sprintf("PEER_STATUS:%s:%s", peerID.String(), status)
}

// RemoveConnection removes a connection
func (cm *ConnectionManager) removeConnection(conn *Connection) {
	// Publish peer leave message before removing the connection
	if cm.topicHandle != nil {
		// leaveMessage := cm.createPeerStatusMessage(conn.PeerID, "LEFT")
		// bytes := []byte(leaveMessage)
		// err := cm.topicHandle.Publish(cm.contextVar, bytes)
		// if err != nil {
		// 	cm.logger.LogToFrontend("ERROR", "Failed to publish leave message: %v", err)
		// }
	}

	// Remove the connection from the slice
	cm.connectionsMutex.Lock()
	for i, c := range cm.connections {
		if c == conn {
			cm.connections = append(cm.connections[:i], cm.connections[i+1:]...)
			break
		}
	}
	cm.connectionsMutex.Unlock()
}

// ConnectToPeer connects to a specific peer
func (cm *ConnectionManager) ConnectToPeer(peerID peer.ID) (*Connection, error) {
	// Start a stream with the destination.
	// Multiaddress of the destination peer is fetched from the peerstore using 'peerId'.
	s, err := cm.host.NewStream(context.Background(), peerID, ProtocolID)
	if err != nil {
		cm.logger.LogToFrontend("ERROR", "Failed to create new stream: %s", err)
		return nil, err
	}
	cm.logger.LogToFrontend("INFO", "Established connection to destination")

	// Create a buffered stream so that read and writes are non-blocking.
	rw := bufio.NewReadWriter(bufio.NewReader(s), bufio.NewWriter(s))

	conn := &Connection{
		RW:       rw,
		PeerID:   peerID,
		LastSeen: time.Now(),
	}

	cm.connectionsMutex.Lock()
	cm.connections = append(cm.connections, conn)
	cm.connectionsMutex.Unlock()

	return conn, nil
}

// GetConnections returns a copy of all connections
func (cm *ConnectionManager) GetConnections() []*Connection {
	cm.connectionsMutex.RLock()
	defer cm.connectionsMutex.RUnlock()

	connections := make([]*Connection, len(cm.connections))
	copy(connections, cm.connections)
	return connections
}

// GetConnectionCount returns the number of active connections
func (cm *ConnectionManager) GetConnectionCount() int {
	cm.connectionsMutex.RLock()
	defer cm.connectionsMutex.RUnlock()
	return len(cm.connections)
}

// AddFailedConnection increments the failed connection count for a peer
func (cm *ConnectionManager) AddFailedConnection(peerID peer.ID) {
	cm.failedConnectionsMutex.Lock()
	cm.failedConnections[peerID]++
	cm.failedConnectionsMutex.Unlock()
}

// GetFailedConnectionCount returns the failed connection count for a peer
func (cm *ConnectionManager) GetFailedConnectionCount(peerID peer.ID) int {
	cm.failedConnectionsMutex.RLock()
	defer cm.failedConnectionsMutex.RUnlock()
	return cm.failedConnections[peerID]
}
