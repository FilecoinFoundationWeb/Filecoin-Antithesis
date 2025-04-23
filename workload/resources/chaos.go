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

var validMultiaddrs = []string{
	"/ip4/127.0.0.1/tcp/1234",
}

var specialChars = []string{
	"/../", "//", "\\", "\x00", "\n", "\t", "\r",
	"!", "@", "#", "$", "%", "^", "&", "*", "?",
}

func generateValidMultiaddr() string {
	if rand.Float32() < 0.2 {
		return validMultiaddrs[rand.Intn(len(validMultiaddrs))]
	}

	var parts []string

	if rand.Float32() < 0.5 {
		ip := fmt.Sprintf("/ip4/%d.%d.%d.%d",
			rand.Intn(256), rand.Intn(256),
			rand.Intn(256), rand.Intn(256))
		parts = append(parts, ip)
	} else {
		ip := fmt.Sprintf("/ip6/%x:%x:%x:%x:%x:%x:%x:%x",
			rand.Intn(65536), rand.Intn(65536), rand.Intn(65536), rand.Intn(65536),
			rand.Intn(65536), rand.Intn(65536), rand.Intn(65536), rand.Intn(65536))
		parts = append(parts, ip)
	}

	port := fmt.Sprintf("/tcp/%d", rand.Intn(65535)+1)
	parts = append(parts, port)

	return strings.Join(parts, "")
}

func generateFuzzedMultiaddr() string {
	if rand.Float32() < 0.3 {
		addr := generateValidMultiaddr()
		log.Printf("Sending valid multiaddr: %s", addr)
		return addr
	}

	mutationType := rand.Intn(5)

	switch mutationType {
	case 0:
		numComponents := rand.Intn(4) + 1
		components := make([]string, numComponents)
		for i := 0; i < numComponents; i++ {
			components[i] = baseComponents[rand.Intn(len(baseComponents))]
		}
		return strings.Join(components, "")

	case 1:
		component := baseComponents[rand.Intn(len(baseComponents))]
		specialChar := specialChars[rand.Intn(len(specialChars))]
		pos := rand.Intn(len(component))
		return component[:pos] + specialChar + component[pos:]

	case 2:
		protocols := []string{"ip4", "ip6", "tcp", "udp", "dns4", "dns6"}
		proto := protocols[rand.Intn(len(protocols))]
		value := strings.Repeat(string(rune(rand.Intn(26)+'a')), rand.Intn(1000)+100)
		return fmt.Sprintf("/%s/%s", proto, value)

	case 3:
		octet := func() string { return fmt.Sprintf("%d", rand.Intn(512)) }
		return fmt.Sprintf("/ip4/%s.%s.%s.%s", octet(), octet(), octet(), octet())

	default:
		port := rand.Intn(70000) + 65535
		return fmt.Sprintf("/tcp/%d", port)
	}
}

type NetworkChaos struct {
	ctx          context.Context
	cancel       context.CancelFunc
	targetAddr   string
	targetInfo   *peer.AddrInfo
	running      bool
	attackMode   string
	pingAttacker *MaliciousPinger
}

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

	pinger, err := NewMaliciousPinger(ctx, targetAddr)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create ping attacker: %w", err)
	}

	return &NetworkChaos{
		ctx:          ctx,
		cancel:       cancel,
		targetAddr:   targetAddr,
		targetInfo:   ai,
		pingAttacker: pinger,
		attackMode:   "random",
	}, nil
}

func (nc *NetworkChaos) Start(minInterval, maxInterval time.Duration) {
	nc.running = true

	if rand.Float32() < 0.5 {
		nc.attackMode = "identify"
		go nc.runFuzzer(minInterval, maxInterval)
	} else {
		nc.attackMode = "ping"
		attackTypes := []PingAttackType{
			RandomPayload,
			OversizedPayload,
			EmptyPayload,
			MultipleStreams,
			IncompleteWrite,
		}
		attackType := attackTypes[rand.Intn(len(attackTypes))]
		concurrency := rand.Intn(5) + 2

		log.Printf("Starting ping chaos with attack type: %d, concurrency: %d",
			attackType, concurrency)
		nc.pingAttacker.Start(attackType, concurrency, minInterval, maxInterval)
	}
}

func (nc *NetworkChaos) Stop() {
	nc.running = false

	if nc.attackMode == "ping" {
		nc.pingAttacker.Stop()
	}

	nc.cancel()
}

func (nc *NetworkChaos) runFuzzer(minInterval, maxInterval time.Duration) {
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

	if err := h.Connect(nc.ctx, *nc.targetInfo); err != nil {
		log.Printf("Failed to connect to target: %v", err)
		return
	}

	log.Printf("Connected to target %s", nc.targetInfo.ID)

	for nc.running {
		stream, err := h.NewStream(nc.ctx, nc.targetInfo.ID, identify.IDPush)
		if err != nil {
			log.Printf("Failed to open stream: %v", err)
			time.Sleep(time.Second)
			continue
		}

		fuzzedAddr := generateFuzzedMultiaddr()
		log.Printf("Sending fuzzed multiaddr: %s", fuzzedAddr)

		writer := protoio.NewDelimitedWriter(stream)
		msg := &pb.Identify{
			ObservedAddr: []byte(fuzzedAddr),
		}

		if err := writer.WriteMsg(msg); err != nil {
			log.Printf("Failed to write message: %v", err)
		}

		stream.Close()

		interval := minInterval
		if maxInterval > minInterval {
			interval = minInterval + time.Duration(rand.Int63n(int64(maxInterval-minInterval)))
		}
		time.Sleep(interval)
	}
}
