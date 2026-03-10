package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"sync"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
)

// IdentityPool manages ephemeral libp2p hosts for the fuzzer.
//
// Stream protocols (ChainExchange, Hello) have zero peer scoring, so a single
// host can be reused indefinitely. GossipSub assigns -1000 score per invalid
// message, so hosts are rotated after a budget is exhausted.
type IdentityPool struct {
	mu      sync.Mutex
	hosts   []host.Host
	budgets map[host.Host]int
	maxPool int

	// Dedicated stream host (reused for exchange/hello attacks)
	streamHost host.Host
}

func newIdentityPool(maxPool int) *IdentityPool {
	return &IdentityPool{
		maxPool: maxPool,
		budgets: make(map[host.Host]int),
	}
}

// createHost creates a new ephemeral libp2p host with a random identity.
func createHost(ctx context.Context) (host.Host, error) {
	priv, _, err := crypto.GenerateKeyPairWithReader(crypto.Ed25519, 0, rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
		libp2p.DisableRelay(),
		libp2p.ResourceManager(&network.NullResourceManager{}),
	)
	if err != nil {
		return nil, fmt.Errorf("create host: %w", err)
	}

	return h, nil
}

// GetForStream returns a reusable host for stream protocols (exchange, hello).
// These protocols have no peer scoring, so one host suffices.
func (p *IdentityPool) GetForStream(ctx context.Context) (host.Host, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.streamHost != nil {
		return p.streamHost, nil
	}

	h, err := createHost(ctx)
	if err != nil {
		return nil, err
	}
	p.streamHost = h
	log.Printf("[protocol-fuzzer] created stream host: %s", h.ID().String()[:16])
	return h, nil
}

// GetForGossip returns a host with remaining GossipSub budget.
// When budget is exhausted, creates a new host (up to maxPool).
func (p *IdentityPool) GetForGossip(ctx context.Context, budget int) (host.Host, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Find a host with remaining budget
	for _, h := range p.hosts {
		if p.budgets[h] > 0 {
			p.budgets[h]--
			return h, nil
		}
	}

	// Evict oldest if at capacity
	if len(p.hosts) >= p.maxPool {
		old := p.hosts[0]
		old.Close()
		delete(p.budgets, old)
		p.hosts = p.hosts[1:]
	}

	// Create new host
	h, err := createHost(ctx)
	if err != nil {
		return nil, err
	}
	p.hosts = append(p.hosts, h)
	p.budgets[h] = budget - 1 // consume one use
	log.Printf("[protocol-fuzzer] created gossip host: %s (budget=%d)", h.ID().String()[:16], budget-1)
	return h, nil
}

// GetFresh always creates a new host. Used for spam/churn attacks.
func (p *IdentityPool) GetFresh(ctx context.Context) (host.Host, error) {
	return createHost(ctx)
}

// CloseAll shuts down all managed hosts.
func (p *IdentityPool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.streamHost != nil {
		p.streamHost.Close()
	}
	for _, h := range p.hosts {
		h.Close()
	}
	p.hosts = nil
	p.budgets = make(map[host.Host]int)
}
