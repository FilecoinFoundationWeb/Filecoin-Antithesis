package resources

import (
	"context"
	"log"

	"github.com/antithesishq/antithesis-sdk-go/assert"
)

// CheckChainBackfill validates the chain index for a given set of nodes.
func CheckChainBackfill(ctx context.Context, nodes []NodeConfig) error {
	for _, node := range nodes {
		log.Printf("[INFO] Performing chain backfill check on node: %s", node.Name)
		api, closer, err := ConnectToNode(ctx, node)
		if err != nil {
			log.Printf("[WARN] Failed to connect to node %s: %v", node.Name, err)
			continue // Move to the next node
		}
		defer closer()

		head, err := api.ChainHead(ctx)
		if err != nil {
			log.Printf("[WARN] Failed to get chain head for node %s: %v", node.Name, err)
			continue
		}

		height := head.Height()
		if height <= 1 {
			log.Printf("[INFO] Chain height too low for backfill test on node %s: %d", node.Name, height)
			continue
		}

		// Test backfill with the previous height
		backfillHeight := height - 1
		_, err = api.ChainValidateIndex(ctx, backfillHeight, true)

		var errMsg string
		if err != nil {
			errMsg = err.Error()
		}

		details := EnhanceAssertDetails(
			map[string]interface{}{
				"error":    errMsg,
				"height":   backfillHeight,
				"property": "Chain index validation",
				"impact":   "High - validates chain index consistency",
				"details":  "Chain index validation ensures proper chain state tracking",
			},
			node.Name,
		)
		assert.Sometimes(err == nil, "[Chain Validation] Chain index validation should succeed", details)

		if err == nil {
			log.Printf("[INFO] Successfully validated chain index backfill for node %s at height %d", node.Name, backfillHeight)
		} else {
			log.Printf("[WARN] Failed to validate chain index backfill for node %s at height %d: %v", node.Name, backfillHeight, err)
		}
	}
	return nil
}
