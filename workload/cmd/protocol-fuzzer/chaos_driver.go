package main

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// ---------------------------------------------------------------------------
// Chaos Driver — Connection and Stream Level Attacks
//
// These patterns target libp2p resource management, connection handling,
// and stream lifecycle — testing crash resilience in code that processes
// untrusted peer behavior.
//
// Targets both Lotus (Go) and Forest (Rust) via shared libp2p protocols.
// ---------------------------------------------------------------------------

func getAllChaosAttacks() []namedAttack {
	return []namedAttack{
		{name: "libp2p/rapid-connect-disconnect", fn: chaosConnectionChurn},
		{name: "libp2p/stream-exhaustion", fn: chaosStreamExhaustion},
		{name: "libp2p/slow-read-backpressure", fn: chaosSlowRead},
		{name: "libp2p/peer-identity-flood", fn: chaosIdentityFlood},
		{name: "libp2p/half-open-streams", fn: chaosHalfOpenStreams},
		{name: "libp2p/bogus-protocol-negotiation", fn: chaosProtocolNegotiation},
	}
}

// chaosConnectionChurn rapidly connects and disconnects from a target.
// Each connection negotiates libp2p protocols and allocates buffers.
// Tests: connection handler cleanup, FD exhaustion, peer manager churn.
func chaosConnectionChurn() {
	target := rngChoice(targets)
	iterations := 30 + rngIntn(40)
	log.Printf("[chaos] connection-churn: %d iterations against %s", iterations, target.Name)

	for i := 0; i < iterations; i++ {
		h, err := pool.GetFresh(ctx)
		if err != nil {
			debugLog("[chaos] churn: create host failed: %v", err)
			continue
		}

		connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = h.Connect(connectCtx, target.AddrInfo)
		cancel()

		if err != nil {
			debugLog("[chaos] churn: connect %d failed: %v", i, err)
		}

		time.Sleep(10 * time.Millisecond)
		h.Close()
	}
}

// chaosStreamExhaustion opens many streams without reading or writing.
// Tests: stream limit enforcement, resource manager, goroutine leak.
func chaosStreamExhaustion() {
	target := rngChoice(targets)
	numStreams := 200 + rngIntn(300)
	log.Printf("[chaos] stream-exhaustion: opening %d streams against %s", numStreams, target.Name)

	h, err := pool.GetFresh(ctx)
	if err != nil {
		log.Printf("[chaos] stream-exhaustion: create host failed: %v", err)
		return
	}
	defer h.Close()

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := h.Connect(connectCtx, target.AddrInfo); err != nil {
		debugLog("[chaos] stream-exhaustion: connect failed: %v", err)
		return
	}

	opened := 0
	for i := 0; i < numStreams; i++ {
		streamCtx, streamCancel := context.WithTimeout(ctx, 2*time.Second)
		s, err := h.NewStream(streamCtx, target.AddrInfo.ID, exchangeProtocol)
		streamCancel()
		if err != nil {
			debugLog("[chaos] stream-exhaustion: hit limit at %d streams: %v", i, err)
			break
		}
		_ = s // intentionally leak — don't close, don't send
		opened++
	}

	log.Printf("[chaos] stream-exhaustion: opened %d streams, holding for 30s", opened)
	time.Sleep(30 * time.Second)
}

// chaosSlowRead opens a ChainExchange stream, sends a valid request,
// then reads the response at 1 byte per second.
// Tests: write timeout enforcement, blocked goroutine accumulation.
func chaosSlowRead() {
	target := rngChoice(targets)
	log.Printf("[chaos] slow-read: targeting %s", target.Name)

	h, err := pool.GetFresh(ctx)
	if err != nil {
		log.Printf("[chaos] slow-read: create host failed: %v", err)
		return
	}
	defer h.Close()

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := h.Connect(connectCtx, target.AddrInfo); err != nil {
		debugLog("[chaos] slow-read: connect failed: %v", err)
		return
	}

	streamCtx, streamCancel := context.WithTimeout(ctx, 10*time.Second)
	defer streamCancel()
	s, err := h.NewStream(streamCtx, target.AddrInfo.ID, exchangeProtocol)
	if err != nil {
		debugLog("[chaos] slow-read: stream failed: %v", err)
		return
	}
	defer s.Close()

	// Send a valid-looking exchange request for 1 block
	request := buildExchangeRequest([]cid.Cid{randomCID()}, 1, 3)
	s.Write(request)
	s.CloseWrite()

	// Read response extremely slowly — target blocks on Write()
	buf := make([]byte, 1)
	duration := 30 + rngIntn(30) // 30-60 seconds
	for i := 0; i < duration; i++ {
		n, err := s.Read(buf)
		if err != nil {
			debugLog("[chaos] slow-read: read stopped after %ds: %v", i, err)
			break
		}
		_ = n
		time.Sleep(1 * time.Second)
	}
	log.Printf("[chaos] slow-read: completed against %s", target.Name)
}

// chaosIdentityFlood creates many peer identities and connects simultaneously.
// Tests: peer table limits, connection manager eviction, memory per-peer.
func chaosIdentityFlood() {
	target := rngChoice(targets)
	numPeers := 50 + rngIntn(50) // 50-100
	log.Printf("[chaos] identity-flood: %d peers against %s", numPeers, target.Name)

	var wg sync.WaitGroup
	connected := 0
	var mu sync.Mutex

	for i := 0; i < numPeers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, err := pool.GetFresh(ctx)
			if err != nil {
				return
			}
			defer h.Close()

			connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if err := h.Connect(connectCtx, target.AddrInfo); err != nil {
				return
			}

			mu.Lock()
			connected++
			mu.Unlock()

			// Hold connection open
			time.Sleep(10 * time.Second)
		}()
	}

	wg.Wait()
	log.Printf("[chaos] identity-flood: %d/%d peers connected to %s", connected, numPeers, target.Name)
}

// chaosHalfOpenStreams opens streams and sends partial garbage data without closing.
// Tests: incomplete CBOR read handling, decoder timeout, resource cleanup.
func chaosHalfOpenStreams() {
	target := rngChoice(targets)
	numStreams := 30 + rngIntn(30)
	log.Printf("[chaos] half-open: %d streams against %s", numStreams, target.Name)

	h, err := pool.GetFresh(ctx)
	if err != nil {
		log.Printf("[chaos] half-open: create host failed: %v", err)
		return
	}
	defer h.Close()

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := h.Connect(connectCtx, target.AddrInfo); err != nil {
		debugLog("[chaos] half-open: connect failed: %v", err)
		return
	}

	opened := 0
	for i := 0; i < numStreams; i++ {
		streamCtx, streamCancel := context.WithTimeout(ctx, 2*time.Second)
		s, err := h.NewStream(streamCtx, target.AddrInfo.ID, exchangeProtocol)
		streamCancel()
		if err != nil {
			debugLog("[chaos] half-open: stream limit at %d: %v", i, err)
			break
		}
		// Write partial garbage — don't close
		garbage := randomBytes(rngIntn(32))
		s.Write(garbage)
		opened++
	}

	log.Printf("[chaos] half-open: %d streams with partial data, holding 30s", opened)
	time.Sleep(30 * time.Second)
}

// chaosProtocolNegotiation attempts to open streams with bogus protocol IDs.
// Tests: multistream-select handler, protocol string parsing.
func chaosProtocolNegotiation() {
	target := rngChoice(targets)
	log.Printf("[chaos] protocol-negotiation: targeting %s", target.Name)

	h, err := pool.GetFresh(ctx)
	if err != nil {
		log.Printf("[chaos] protocol-negotiation: create host failed: %v", err)
		return
	}
	defer h.Close()

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := h.Connect(connectCtx, target.AddrInfo); err != nil {
		debugLog("[chaos] protocol-negotiation: connect failed: %v", err)
		return
	}

	bogusProtocols := []protocol.ID{
		"/fil/chain/xchg/99.99.99",
		"/fil/hello/99.0.0",
		protocol.ID(strings.Repeat("/a", 5000)), // 10KB protocol string
		"",
		"/\x00\xff\xfe",
		"/fil/blocks/\x00\x00\x00",
		protocol.ID(string(randomBytes(256))),
	}

	for _, proto := range bogusProtocols {
		streamCtx, streamCancel := context.WithTimeout(ctx, 2*time.Second)
		s, err := h.NewStream(streamCtx, target.AddrInfo.ID, proto)
		streamCancel()
		if err == nil {
			debugLog("[chaos] protocol-negotiation: unexpectedly opened stream for %q", string(proto))
			s.Close()
		}
	}
	log.Printf("[chaos] protocol-negotiation: tested %d bogus protocols", len(bogusProtocols))
}
