# P2PChat
P2PChat is a cross-platform peer-to-peer chat application that combines a Go backend (using libp2p for networking) with a C# console frontend. The project enables users to connect, discover peers, and exchange messages directly over a decentralized network, without relying on centralized servers.

Features:
- Decentralized peer discovery and messaging using libp2p
- Real-time chat with colored and formatted console output
- Cross-platform: Go backend and .NET (C#) frontend
- Supports bootstrap nodes and relay peers for NAT traversal
- Clean separation between backend and frontend for easy extensibility

How it works:
- The C# frontend launches and communicates with the Go backend via HTTP.
- The Go backend manages all P2P networking, peer discovery, and message relaying.
- Messages are displayed in the console with clear formatting, and user input is handled responsively.

Getting Started:
- Build the Go backend (go build -o chatp2p ./go-code)
- Run the C# frontend (dotnet run --project P2PChat.Frontend -- -port port-num -bootstrap "relay-address")
- Connect to peers and start chatting!
