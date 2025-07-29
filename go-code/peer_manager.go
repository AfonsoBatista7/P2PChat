package main

// PeerManager struct to manage peer state
type PeerManager struct {
	done        chan bool // Channel to signal termination
	disconnect  chan bool // Channel to signal termination
	unsubscribe chan bool // Channel to signal termination
}
