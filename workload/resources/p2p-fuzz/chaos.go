package p2pfuzz

import (
	"context"
	"fmt"
	"log"
	"math/rand"
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

// NetworkChaos performs various network-level attacks or disruptions against a target node.
type NetworkChaos struct {
	ctx          context.Context
	cancel       context.CancelFunc
	targetAddr   string
	targetInfo   *peer.AddrInfo
	running      bool
	attackMode   string // "identify" or "ping"
	pingAttacker *MaliciousPinger
}

// NewNetworkChaos initializes a NetworkChaos instance.
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
		attackMode:   "random", // Default mode, decided in Start()
	}, nil
}

// Start begins the chaos operation, choosing between identify fuzzing or ping attacks.
func (nc *NetworkChaos) Start(minInterval, maxInterval time.Duration) {
	nc.running = true

	if rand.Float32() < 0.5 { // 50% chance to run identify fuzzer
		nc.attackMode = "identify"
		log.Printf("Starting identify protocol fuzzing against %s", nc.targetInfo.ID)
		go nc.runIdentifyFuzzer(minInterval, maxInterval)
	} else { // 50% chance to run ping attacks
		nc.attackMode = "ping"
		// Select a random ping attack type (can be expanded)
		attackTypes := []PingAttackType{
			RandomPayload,
			OversizedPayload,
			EmptyPayload,
			MultipleStreams,
			IncompleteWrite,
			PingBarrage,
			MalformedPayload,
			ConnectDisconnect,
			VariablePayload,
			SlowWrite,
		}
		attackType := attackTypes[rand.Intn(len(attackTypes))]
		concurrency := rand.Intn(5) + 2 // 2-6 concurrent pings

		log.Printf("Starting ping chaos against %s with attack type: %d, concurrency: %d",
			nc.targetInfo.ID, attackType, concurrency)
		nc.pingAttacker.Start(attackType, concurrency, minInterval, maxInterval)
	}
}

// Stop terminates the running chaos operation.
func (nc *NetworkChaos) Stop() {
	nc.running = false

	if nc.attackMode == "ping" {
		nc.pingAttacker.Stop()
	}

	nc.cancel() // Cancel the context for identify fuzzer if running
	log.Printf("Network chaos stopped for target %s", nc.targetInfo.ID)
}

// runIdentifyFuzzer periodically sends fuzzed multiaddrs via the Identify protocol.
func (nc *NetworkChaos) runIdentifyFuzzer(minInterval, maxInterval time.Duration) {
	priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, -1)
	if err != nil {
		log.Printf("[ERROR] Identify Fuzzer: Failed to generate key pair: %v", err)
		return
	}

	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
		libp2p.ResourceManager(&network.NullResourceManager{}),
	)
	if err != nil {
		log.Printf("[ERROR] Identify Fuzzer: Failed to create host: %v", err)
		return
	}
	defer h.Close()

	if err := h.Connect(nc.ctx, *nc.targetInfo); err != nil {
		log.Printf("[WARN] Identify Fuzzer: Failed to connect to target %s: %v", nc.targetInfo.ID, err)
		// Don't return immediately, maybe connection will work later
	}

	log.Printf("Identify Fuzzer: Connected to target %s", nc.targetInfo.ID)
	ticker := time.NewTicker(minInterval)
	defer ticker.Stop()

	for nc.running {
		select {
		case <-nc.ctx.Done():
			log.Printf("Identify Fuzzer: Context cancelled, stopping.")
			return
		case <-ticker.C:
			stream, err := h.NewStream(nc.ctx, nc.targetInfo.ID, identify.IDPush)
			if err != nil {
				// Log failure to open stream, but continue loop
				log.Printf("[WARN] Identify Fuzzer: Failed to open stream to %s: %v", nc.targetInfo.ID, err)
				time.Sleep(time.Second) // Brief pause before retry
				continue
			}

			fuzzedAddr := generateFuzzedMultiaddr() // Use the function from multiaddr_fuzz.go
			log.Printf("Identify Fuzzer: Sending fuzzed multiaddr: %s", fuzzedAddr)

			writer := protoio.NewDelimitedWriter(stream)
			msg := &pb.Identify{
				ObservedAddr: []byte(fuzzedAddr),
			}

			if err := writer.WriteMsg(msg); err != nil {
				log.Printf("[WARN] Identify Fuzzer: Failed to write message to %s: %v", nc.targetInfo.ID, err)
				// Close stream on write error, attempt new stream next tick
				stream.Reset()
			} else {
				stream.CloseWrite() // Close write side after successful send
			}

			// Reset ticker for variable interval
			if maxInterval > minInterval {
				interval := minInterval + time.Duration(rand.Int63n(int64(maxInterval-minInterval)))
				ticker.Reset(interval)
			}
		}
	}
	log.Printf("Identify Fuzzer: Loop finished for target %s", nc.targetInfo.ID)
}
