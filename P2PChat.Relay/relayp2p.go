package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"sync"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/multiformats/go-multiaddr"
)

func makeHost(randomness io.Reader) (host.Host, error) {
	// Creates a new RSA key pair for this host.
	prvKey, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, randomness)
	if err != nil {
		fmt.Printf("Failed to generate private key: %s\n", err)
		return nil, err
	}

	sourceMultiAddrTCP, _ := multiaddr.NewMultiaddr("/ip4/0.0.0.0/tcp/4001/")
	sourceMultiAddrUDP, _ := multiaddr.NewMultiaddr("/ip4/0.0.0.0/udp/4001/quic-v1")

	// libp2p.New constructs a new libp2p Host.
	// Other options can be added here.
	return libp2p.New(
		libp2p.ListenAddrs(sourceMultiAddrTCP, sourceMultiAddrUDP),
		libp2p.Identity(prvKey),

		// Attempt to open ports using uPNP for NATed hosts.
		libp2p.NATPortMap(),
		libp2p.EnableHolePunching(),
		libp2p.EnableNATService(),

		libp2p.EnableRelayService(),
	)
}

func main() {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var r io.Reader = rand.Reader

	relayData, err := makeHost(r)
	if err != nil {
		panic(err)
	}

	_, err = relay.New(relayData)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Relay Address ->%s/p2p/%s\n", relayData.Addrs()[len(relayData.Addrs())-1], relayData.ID())

	for addr := range relayData.Addrs() {
		fmt.Printf("Addr -> %s\n", relayData.Addrs()[addr])
	}

	kademliaDht, err := dht.New(ctx, relayData, dht.Mode(dht.ModeServer))
	if err != nil {
		panic(err)
	}

	// Bootstrap the DHT. In the default configuration, this spawns a Background
	// thread that will refresh the peer table every five minutes.
	if err = kademliaDht.Bootstrap(ctx); err != nil {
		panic(err)
	}

	var wg sync.WaitGroup

	connectBootstrapPeer(ctx, relayData, kademliaDht, &wg)

	fmt.Println("Peer Started!")

	// Wait until the peer is terminated
	select {}
}

func connectBootstrapPeer(ctx context.Context, relayData host.Host, kademliaDht *dht.IpfsDHT,wg *sync.WaitGroup) {
	wg.Add(1)

	go func () {
		defer wg.Done()

		discovery := routing.NewRoutingDiscovery(kademliaDht)
		util.Advertise(ctx, discovery, relayData.ID().String())

	}()
}
