package liveness

import (
	"context"
	"log"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/filecoin-project/go-state-types/abi"
)

// CheckChainProgress verifies that the chain makes progress over time.
// This is a liveness property: the chain should advance at some point.
func CheckChainProgress(ctx context.Context, c *resources.Client, window time.Duration) error {
	startHead, err := c.API.ChainHead(ctx)
	if err != nil {
		log.Printf("[liveness] Failed to get initial chain head from %s: %v", c.Node.ID, err)
		return nil
	}

	startHeight := startHead.Height()
	log.Printf("[liveness] %s starting height: %d, waiting %v", c.Node.ID, startHeight, window)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(window):
	}

	endHead, err := c.API.ChainHead(ctx)
	if err != nil {
		log.Printf("[liveness] Failed to get final chain head from %s: %v", c.Node.ID, err)
		return nil
	}

	endHeight := endHead.Height()
	progressed := endHeight > startHeight

	resources.Sometimes(progressed, "chain_progresses", map[string]any{
		"node":           c.Node.ID,
		"start_height":   int64(startHeight),
		"end_height":     int64(endHeight),
		"window_seconds": window.Seconds(),
	})

	if progressed {
		log.Printf("[liveness] ✓ %s progressed: %d -> %d (+%d blocks)",
			c.Node.ID, startHeight, endHeight, endHeight-startHeight)
	} else {
		log.Printf("[liveness] ✗ %s did not progress: stayed at %d", c.Node.ID, startHeight)
	}

	return nil
}

// CheckAnyNodeProgress verifies that at least one node makes progress.
func CheckAnyNodeProgress(ctx context.Context, pool *resources.ClientPool, window time.Duration) error {
	clients := pool.All()

	// Record starting heights
	startHeights := make(map[string]abi.ChainEpoch)
	for _, c := range clients {
		head, err := c.API.ChainHead(ctx)
		if err != nil {
			continue
		}
		startHeights[c.Node.ID] = head.Height()
	}

	log.Printf("[liveness] Starting heights recorded for %d nodes, waiting %v", len(startHeights), window)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(window):
	}

	// Check if any node progressed
	anyProgressed := false
	for _, c := range clients {
		startHeight, ok := startHeights[c.Node.ID]
		if !ok {
			continue
		}

		head, err := c.API.ChainHead(ctx)
		if err != nil {
			continue
		}

		if head.Height() > startHeight {
			anyProgressed = true
			log.Printf("[liveness] ✓ %s progressed: %d -> %d",
				c.Node.ID, startHeight, head.Height())
		}
	}

	resources.Sometimes(anyProgressed, "network_makes_progress", map[string]any{
		"nodes_checked":  len(startHeights),
		"window_seconds": window.Seconds(),
	})

	return nil
}
