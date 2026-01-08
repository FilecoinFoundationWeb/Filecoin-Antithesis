package state

import (
	"context"
	"log"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
)

// CheckStateRootMatch verifies that all nodes compute the same state root
// for finalized tipsets. This is a critical determinism property.
func CheckStateRootMatch(ctx context.Context, pool *resources.ClientPool) error {
	clients := pool.All()
	if len(clients) < 2 {
		log.Println("[state] Skipping state root check: need at least 2 nodes")
		return nil
	}

	// Get a finalized tipset that all nodes should have
	finalized, err := getCommonFinalizedTipset(ctx, clients)
	if err != nil {
		log.Printf("[state] Failed to get common finalized tipset: %v", err)
		return nil
	}

	// Compute state root on each node
	var referenceRoot string
	var referenceNode string
	allMatch := true

	for _, c := range clients {
		// Use StateCompute to get the state root after applying messages
		result, err := c.API.StateCompute(ctx, finalized.Height(), nil, finalized.Key())
		if err != nil {
			log.Printf("[state] Failed to compute state on %s: %v", c.Node.ID, err)
			continue
		}

		rootStr := result.Root.String()
		if referenceRoot == "" {
			referenceRoot = rootStr
			referenceNode = c.Node.ID
		} else if rootStr != referenceRoot {
			allMatch = false
			log.Printf("[state] MISMATCH: %s has root %s, %s has root %s",
				referenceNode, referenceRoot, c.Node.ID, rootStr)
		}
	}

	resources.Always(allMatch, "state_root_deterministic", map[string]any{
		"height":         int64(finalized.Height()),
		"tipset":         finalized.Key().String(),
		"reference_node": referenceNode,
		"reference_root": referenceRoot,
		"nodes_checked":  len(clients),
	})

	if allMatch {
		log.Printf("[state] ✓ All nodes agree on state root at height %d", finalized.Height())
	}

	return nil
}

// getCommonFinalizedTipset finds a tipset that should be finalized on all nodes.
// Uses ChainGetFinalizedTipSet which handles F3/EC finality fallback internally.
func getCommonFinalizedTipset(ctx context.Context, clients []*resources.Client) (*types.TipSet, error) {
	// Get the minimum finalized height across all nodes
	var minHeight abi.ChainEpoch = -1

	for _, c := range clients {
		ts, err := c.API.ChainGetFinalizedTipSet(ctx)
		if err != nil {
			continue
		}
		if minHeight == -1 || ts.Height() < minHeight {
			minHeight = ts.Height()
		}
	}

	if minHeight < 0 {
		return nil, nil
	}

	// Get the tipset at the minimum finalized height from the first available client
	for _, c := range clients {
		ts, err := c.API.ChainGetTipSetByHeight(ctx, minHeight, types.EmptyTSK)
		if err == nil {
			return ts, nil
		}
	}

	return nil, nil
}
