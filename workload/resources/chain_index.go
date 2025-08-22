package resources

import (
	"context"
	"log"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/lotus/chain/types"
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

		details := map[string]interface{}{
			"error":    errMsg,
			"height":   backfillHeight,
			"property": "Chain index validation",
			"impact":   "High - validates chain index consistency",
			"details":  "Chain index validation ensures proper chain state tracking",
		}
		assert.Sometimes(err == nil, "[Chain Validation] Chain index validation should succeed", details)

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
		assert.Sometimes(true, "Chain index backfill check completed.", map[string]interface{}{"requirement": "Chain index backfill check completed."})
		log.Println("[INFO] Chain index backfill check completed.")
		return nil
	}, "Chain index backfill check operation")
}

// TestJsonRPC tests JSON-RPC functionality
func TestJsonRPC(ctx context.Context) error {
	forestNode := NodeConfig{Name: "Forest", RPCURL: "http://10.20.20.28:3456", AuthTokenPath: "/root/devgen/forest/jwt"}
	api, closer, err := ConnectToNode(ctx, forestNode)
	if err != nil {
		log.Println(err)
	}
	defer closer()
	ts, err := api.ChainGetTipSet(ctx, types.EmptyTSK)
	if err != nil {
		log.Println(err)
		return err
	}
	log.Printf("[INFO] Forest node tipset: %v", ts)
	return nil
}
