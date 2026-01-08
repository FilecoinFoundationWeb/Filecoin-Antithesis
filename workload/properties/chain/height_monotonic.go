package chain

import (
	"context"
	"log"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/filecoin-project/go-state-types/abi"
)

// HeightTracker tracks chain head heights per node for monotonicity checks.
type HeightTracker struct {
	heights map[string]abi.ChainEpoch
}

// NewHeightTracker creates a new height tracker.
func NewHeightTracker() *HeightTracker {
	return &HeightTracker{
		heights: make(map[string]abi.ChainEpoch),
	}
}

// CheckHeightMonotonic verifies that chain head height never decreases.
// This should be called periodically on each node.
func (t *HeightTracker) CheckHeightMonotonic(ctx context.Context, c *resources.Client) error {
	head, err := c.API.ChainHead(ctx)
	if err != nil {
		log.Printf("[chain] Failed to get chain head from %s: %v", c.Node.ID, err)
		return nil // Don't fail the check on transient errors
	}

	currentHeight := head.Height()
	prevHeight, exists := t.heights[c.Node.ID]

	if exists {
		monotonic := currentHeight >= prevHeight

		resources.Always(monotonic, "chain_height_never_decreases", map[string]any{
			"node":            c.Node.ID,
			"previous_height": int64(prevHeight),
			"current_height":  int64(currentHeight),
		})

		if !monotonic {
			log.Printf("[chain] ✗ Height decreased on %s: %d -> %d", c.Node.ID, prevHeight, currentHeight)
		} else {
			log.Printf("[chain] ✓ %s height: %d (was %d)", c.Node.ID, currentHeight, prevHeight)
		}
	} else {
		log.Printf("[chain] %s initial height: %d", c.Node.ID, currentHeight)
	}

	t.heights[c.Node.ID] = currentHeight
	return nil
}

// CheckAllNodesMonotonic checks height monotonicity for all nodes.
func (t *HeightTracker) CheckAllNodesMonotonic(ctx context.Context, pool *resources.ClientPool) error {
	for _, c := range pool.All() {
		if err := t.CheckHeightMonotonic(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

// MonitorHeightMonotonic continuously checks height monotonicity.
func (t *HeightTracker) MonitorHeightMonotonic(ctx context.Context, pool *resources.ClientPool, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.CheckAllNodesMonotonic(ctx, pool)
		}
	}
}
