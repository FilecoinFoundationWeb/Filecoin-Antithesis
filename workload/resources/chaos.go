package resources

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	pb "github.com/libp2p/go-libp2p/p2p/protocol/identify/pb"
	"github.com/libp2p/go-msgio/protoio"
	"github.com/multiformats/go-multiaddr"
)

// Base multiaddr components for mutation
var baseComponents = []string{
	"/ip4/127.0.0.1",
	"/ip6/::1",
	"/ip6zone/eth0",
	"/tcp/1234",
	"/udp/1234",
	"/dns4/localhost",
	"/dns6/localhost",
	"/dnsaddr/localhost",
	"/p2p/QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N",
}

// Valid multiaddr templates
var validMultiaddrs = []string{
	"/ip4/127.0.0.1/tcp/1234",
	"/ip4/127.0.0.1/udp/1234/quic",
	"/ip6/::1/tcp/1234",
	"/dns4/bootstrap.libp2p.io/tcp/443/wss",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN",
}

// Special characters for mutations
var specialChars = []string{
	"/../", "//", "\\", "\x00", "\n", "\t", "\r",
	"!", "@", "#", "$", "%", "^", "&", "*", "?",
}

// Generate a valid multiaddr
func generateValidMultiaddr() string {
	// 20% chance to use a template directly
	if rand.Float32() < 0.2 {
		return validMultiaddrs[rand.Intn(len(validMultiaddrs))]
	}

	// Otherwise construct a valid one
	var parts []string

	// Add IP address
	if rand.Float32() < 0.5 {
		// IPv4
		ip := fmt.Sprintf("/ip4/%d.%d.%d.%d",
			rand.Intn(256), rand.Intn(256),
			rand.Intn(256), rand.Intn(256))
		parts = append(parts, ip)
	} else {
		// IPv6
		ip := fmt.Sprintf("/ip6/%x:%x:%x:%x:%x:%x:%x:%x",
			rand.Intn(65536), rand.Intn(65536),
			rand.Intn(65536), rand.Intn(65536),
			rand.Intn(65536), rand.Intn(65536),
			rand.Intn(65536), rand.Intn(65536))
		parts = append(parts, ip)
	}

	// Add port
	port := rand.Intn(65535) + 1
	if rand.Float32() < 0.7 {
		parts = append(parts, fmt.Sprintf("/tcp/%d", port))
	} else {
		parts = append(parts, fmt.Sprintf("/udp/%d", port))
		if rand.Float32() < 0.5 {
			parts = append(parts, "/quic")
		}
	}

	return strings.Join(parts, "")
}

// Generate a mutated multiaddr
func generateFuzzedMultiaddr() string {
	// 30% chance to send valid multiaddr
	if rand.Float32() < 0.3 {
		addr := generateValidMultiaddr()
		log.Printf("Sending valid multiaddr: %s", addr)
		return addr
	}

	mutationType := rand.Intn(5)

	switch mutationType {
	case 0:
		// Combine random components
		numComponents := rand.Intn(4) + 1
		components := make([]string, numComponents)
		for i := 0; i < numComponents; i++ {
			components[i] = baseComponents[rand.Intn(len(baseComponents))]
		}
		return strings.Join(components, "")

	case 1:
		// Insert special characters
		component := baseComponents[rand.Intn(len(baseComponents))]
		specialChar := specialChars[rand.Intn(len(specialChars))]
		pos := rand.Intn(len(component))
		return component[:pos] + specialChar + component[pos:]

	case 2:
		// Generate oversized values
		protocols := []string{"ip4", "ip6", "tcp", "udp", "dns4", "dns6"}
		proto := protocols[rand.Intn(len(protocols))]
		value := strings.Repeat(string(rune(rand.Intn(26)+'a')), rand.Intn(1000)+100)
		return fmt.Sprintf("/%s/%s", proto, value)

	case 3:
		// Create malformed IP addresses
		octet := func() string { return fmt.Sprintf("%d", rand.Intn(512)) }
		return fmt.Sprintf("/ip4/%s.%s.%s.%s", octet(), octet(), octet(), octet())

	default:
		// Random port numbers
		port := rand.Intn(70000) + 65535 // Generate ports beyond valid range
		return fmt.Sprintf("/tcp/%d", port)
	}
}

// NetworkChaos manages chaos operations targeting Lotus nodes
type NetworkChaos struct {
	ctx        context.Context
	cancel     context.CancelFunc
	targetAddr string
	targetInfo *peer.AddrInfo
	running    bool
}

// NewNetworkChaos creates a new chaos manager targeting the specified node
func NewNetworkChaos(ctx context.Context, targetAddr string) (*NetworkChaos, error) {
	ctx, cancel := context.WithCancel(ctx)

	targetMaddr, err := multiaddr.NewMultiaddr(targetAddr)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to parse target multiaddr: %w", err)
	}

	ai, err := peer.AddrInfoFromP2pAddr(targetMaddr)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to get addr info: %w", err)
	}

	return &NetworkChaos{
		ctx:        ctx,
		cancel:     cancel,
		targetAddr: targetAddr,
		targetInfo: ai,
	}, nil
}

// Start begins chaos operations
func (nc *NetworkChaos) Start(minInterval, maxInterval time.Duration) {
	nc.running = true
	go nc.runFuzzer(minInterval, maxInterval)
}

// Stop halts all chaos operations
func (nc *NetworkChaos) Stop() {
	nc.running = false
	nc.cancel()
}

func (nc *NetworkChaos) runFuzzer(minInterval, maxInterval time.Duration) {
	// Create a host for fuzzing
	priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, -1)
	if err != nil {
		log.Printf("Failed to generate key pair: %v", err)
		return
	}

	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
		libp2p.ResourceManager(&network.NullResourceManager{}),
	)
	if err != nil {
		log.Printf("Failed to create host: %v", err)
		return
	}
	defer h.Close()

	// Connect to target
	if err := h.Connect(nc.ctx, *nc.targetInfo); err != nil {
		log.Printf("Failed to connect to target: %v", err)
		return
	}

	log.Printf("Connected to target %s", nc.targetInfo.ID)

	// Run fuzzing loop
	for nc.running {
		// Open identify push stream
		stream, err := h.NewStream(nc.ctx, nc.targetInfo.ID, identify.IDPush)
		if err != nil {
			log.Printf("Failed to open stream: %v", err)
			time.Sleep(time.Second)
			continue
		}

		// Generate and log fuzzed multiaddr
		fuzzedAddr := generateFuzzedMultiaddr()
		log.Printf("Sending fuzzed multiaddr: %s", fuzzedAddr)

		// Send identify push message
		writer := protoio.NewDelimitedWriter(stream)
		msg := &pb.Identify{
			ObservedAddr: []byte(fuzzedAddr),
		}

		if err := writer.WriteMsg(msg); err != nil {
			log.Printf("Failed to write message: %v", err)
		}

		stream.Close()

		// Wait for random interval
		interval := minInterval
		if maxInterval > minInterval {
			interval = minInterval + time.Duration(rand.Int63n(int64(maxInterval-minInterval)))
		}
		time.Sleep(interval)
	}
}
