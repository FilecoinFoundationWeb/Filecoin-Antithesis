package resources

import (
	"context"
	"fmt"
	"log"
)

const (
	// MinHeightForBackfillTest is the minimum chain height required to perform backfill validation
	MinHeightForBackfillTest = 20
	// BackfillDepth is how many blocks behind the head to validate
	BackfillDepth = 5
)

// CheckChainBackfill validates the chain index for a given set of nodes.
func CheckChainBackfill(ctx context.Context, nodes []NodeConfig) error {
	for _, node := range nodes {
		if err := validateNodeChainIndex(ctx, node); err != nil {
			log.Printf("[WARN] Chain index validation failed for node %s: %v", node.Name, err)
			// Continue to next node instead of failing entirely
		}
	}
	return nil
}

// validateNodeChainIndex performs chain index validation for a single node
func validateNodeChainIndex(ctx context.Context, node NodeConfig) error {
	log.Printf("[INFO] Performing chain backfill check on node: %s", node.Name)

	api, closer, err := ConnectToNode(ctx, node)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer closer() // Now safe - one defer per function call

	head, err := api.ChainHead(ctx)
	if err != nil {
		return fmt.Errorf("failed to get chain head: %w", err)
	}

	height := head.Height()
	if height <= MinHeightForBackfillTest {
		log.Printf("[INFO] Chain height too low for backfill test on node %s: %d", node.Name, height)
		return nil // Not an error, just too early
	}

	backfillHeight := height - BackfillDepth
	_, err = api.ChainValidateIndex(ctx, backfillHeight, true)

	AssertSometimes(node.Name, err == nil, "Chain index validation should succeed", map[string]interface{}{
		"operation":        "chain_index_validation",
		"height":           backfillHeight,
		"validation_error": fmt.Sprintf("%v", err),
	})

	if err != nil {
		log.Printf("[WARN] Failed to validate chain index backfill for node %s at height %d: %v", node.Name, backfillHeight, err)
		return fmt.Errorf("chain index validation failed at height %d: %w", backfillHeight, err)
	}

	log.Printf("[INFO] Successfully validated chain index backfill for node %s at height %d", node.Name, backfillHeight)
	return nil
}

// PerformCheckBackfill checks chain index backfill for all Lotus nodes
func PerformCheckBackfill(ctx context.Context, config *Config) error {
	log.Println("[INFO] Starting chain index backfill check...")

	// Only use Lotus nodes - Forest doesn't support ChainValidateIndex
	filteredNodes := FilterLotusNodes(config.Nodes)
	if len(filteredNodes) == 0 {
		log.Printf("[WARN] No Lotus nodes found in config, skipping backfill check")
		return nil
	}

	return RetryOperation(ctx, func() error {
		err := CheckChainBackfill(ctx, filteredNodes)
		if err != nil {
			log.Printf("[WARN] Chain backfill check failed, will retry: %v", err)
			return err
		}
		log.Println("[INFO] Chain index backfill check completed.")
		return nil
	}, "Chain index backfill check operation")
}
