package finalization

import (
	"context"
	"log"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
)

// CheckFinalizedTipsetMatch verifies that all nodes agree on finalized tipsets.
// This is the most critical safety property: once a tipset is finalized,
// all nodes must report the same CID for that height.
func CheckFinalizedTipsetMatch(ctx context.Context, pool *resources.ClientPool) error {
	clients := pool.All()
	if len(clients) < 2 {
		log.Println("[finalization] Skipping tipset match check: need at least 2 nodes")
		return nil
	}

	// Collect finalized tipsets from each node
	type nodeFinalized struct {
		nodeID string
		tipset *types.TipSet
		err    error
	}

	results := make([]nodeFinalized, len(clients))
	for i, c := range clients {
		ts, err := getLatestFinalized(ctx, c)
		results[i] = nodeFinalized{
			nodeID: c.Node.ID,
			tipset: ts,
			err:    err,
		}
	}

	// Find the minimum finalized height across all nodes
	var minHeight int64 = -1
	for _, r := range results {
		if r.err != nil {
			log.Printf("[finalization] Failed to get finalized tipset from %s: %v", r.nodeID, r.err)
			continue
		}
		h := int64(r.tipset.Height())
		if minHeight == -1 || h < minHeight {
			minHeight = h
		}
	}

	if minHeight < 0 {
		log.Println("[finalization] No valid finalized tipsets found")
		return nil
	}

	// Get tipset at minHeight from all nodes and compare
	var referenceCID string
	var referenceNode string
	allMatch := true

	for _, c := range clients {
		ts, err := c.API.ChainGetTipSetByHeight(ctx, abi.ChainEpoch(minHeight), types.EmptyTSK)
		if err != nil {
			log.Printf("[finalization] Failed to get tipset at height %d from %s: %v", minHeight, c.Node.ID, err)
			continue
		}

		cidStr := ts.Key().String()
		if referenceCID == "" {
			referenceCID = cidStr
			referenceNode = c.Node.ID
		} else if cidStr != referenceCID {
			allMatch = false
			log.Printf("[finalization] MISMATCH: %s has %s, %s has %s at height %d",
				referenceNode, referenceCID, c.Node.ID, cidStr, minHeight)
		}
	}

	resources.Always(allMatch, "finalized_tipsets_match", map[string]any{
		"height":         minHeight,
		"reference_node": referenceNode,
		"reference_cid":  referenceCID,
		"nodes_checked":  len(clients),
	})

	if allMatch {
		log.Printf("[finalization] ✓ All nodes agree on tipset at finalized height %d", minHeight)
	}

	return nil
}

// getLatestFinalized returns the latest finalized tipset from a node.
// Uses ChainGetFinalizedTipSet which handles F3/EC finality fallback internally.
func getLatestFinalized(ctx context.Context, c *resources.Client) (*types.TipSet, error) {
	return c.API.ChainGetFinalizedTipSet(ctx)
}
