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
			return nil
		}

		height := head.Height()
		if height <= 20 {
			log.Printf("[INFO] Chain height too low for backfill test on node %s: %d", node.Name, height)
			return nil
		}

		// Test backfill with the previous height
		backfillHeight := height - 5
		_, err = api.ChainValidateIndex(ctx, backfillHeight, true)

		var errMsg string
		if err != nil {
			errMsg = err.Error()
		}

		details := map[string]interface{}{
			"error":    errMsg,
			"height":   backfillHeight,
			"property": "Chain index validation",
			"impact":   "High - validates chain index consistency",
			"details":  "Chain index validation ensures proper chain state tracking",
		}
		assert.Sometimes(err == nil, "Chain index validation: Chain index validation should succeed - validation failure detected", map[string]interface{}{
			"operation":   "chain_index_validation",
			"requirement": "Chain index validation should succeed",
			"details":     details,
		})

		if err == nil {
			log.Printf("[INFO] Successfully validated chain index backfill for node %s at height %d", node.Name, backfillHeight)
		} else {
			log.Printf("[WARN] Failed to validate chain index backfill for node %s at height %d: %v", node.Name, backfillHeight, err)
		}
	}
	return nil
}

// PerformCheckBackfill checks chain index backfill
func PerformCheckBackfill(ctx context.Context, config *Config) error {
	log.Println("[INFO] Starting chain index backfill check...")

	// Filter nodes to "Lotus1" and "Lotus2"
	filteredNodes := FilterLotusNodes(config.Nodes)

	if len(filteredNodes) == 0 {
		log.Printf("[ERROR] No Lotus nodes found in config")
		return nil
	}

	return RetryOperation(ctx, func() error {
		err := CheckChainBackfill(ctx, filteredNodes)
		if err != nil {
			log.Printf("[WARN] Chain backfill check failed, will retry: %v", err)
			return err // Return original error for retry
		}
		assert.Sometimes(true, "Chain index backfill: Chain index backfill check completed successfully", map[string]interface{}{
			"operation":   "chain_index_backfill",
			"requirement": "Chain index backfill check completed.",
		})
		log.Println("[INFO] Chain index backfill check completed.")
		return nil
	}, "Chain index backfill check operation")
}
