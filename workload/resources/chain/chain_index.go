package chain

import (
	"context"
	"log"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources/connect"
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

// CheckChainBackfill validates the chain index for a given set of nodes.
func CheckChainBackfill(ctx context.Context, nodes []connect.NodeConfig) error {
	for _, node := range nodes {
		log.Printf("[INFO] Performing chain backfill check on node: %s", node.Name)
		api, closer, err := connect.ConnectToNode(ctx, node)
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
		if height <= 20 {
			log.Printf("[INFO] Chain height too low for backfill test on node %s: %d", node.Name, height)
			continue
		}
		backfillHeight := height - 5

		var validationErr error
		maxRetries := 3
		for attempt := 1; attempt <= maxRetries; attempt++ {
			_, validationErr = api.ChainValidateIndex(ctx, backfillHeight, true)

			if validationErr == nil {
				log.Printf("[INFO] Chain validation succeeded on attempt %d for node %s at height %d", attempt, node.Name, backfillHeight)
				break
			}

			if attempt < maxRetries {
				log.Printf("[INFO] Chain validation failed on attempt %d for node %s at height %d, retrying... Error: %v", attempt, node.Name, backfillHeight, validationErr)
			}
		}

		var errMsg string
		if validationErr != nil {
			errMsg = validationErr.Error()
		}

		details := map[string]interface{}{
			"error":    errMsg,
			"height":   backfillHeight,
			"property": "Chain index validation",
			"impact":   "High - validates chain index consistency",
			"details":  "Chain index validation ensures proper chain state tracking",
			"retries":  maxRetries,
		}
		assert.Sometimes(validationErr == nil, "[Chain Validation] Chain index validation should succeed", details)

		if validationErr == nil {
			log.Printf("[INFO] Successfully validated chain index backfill for node %s at height %d", node.Name, backfillHeight)
		} else {
			log.Printf("[WARN] Failed to validate chain index backfill for node %s at height %d after %d attempts: %v", node.Name, backfillHeight, maxRetries, validationErr)
		}
	}
	return nil
}

// PerformCheckBackfill checks chain index backfill
func PerformCheckBackfill(ctx context.Context, config *connect.Config) error {
	log.Println("[INFO] Starting chain index backfill check...")

	// Filter nodes to "Lotus1" and "Lotus2"
	filteredNodes := connect.FilterLotusNodes(config.Nodes)

	if len(filteredNodes) == 0 {
		log.Printf("[ERROR] No Lotus nodes found in config")
		return nil
	}

	return connect.RetryOperation(ctx, func() error {
		err := CheckChainBackfill(ctx, filteredNodes)
		if err != nil {
			log.Printf("[WARN] Chain backfill check failed, will retry: %v", err)
			return err // Return original error for retry
		}
		assert.Sometimes(true, "Chain index backfill check completed.", map[string]interface{}{"requirement": "Chain index backfill check completed."})
		log.Println("[INFO] Chain index backfill check completed.")
		return nil
	}, "Chain index backfill check operation")
}
