package resources

import (
	"context"
	"fmt"
	"log"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
)

const (
	// MinHeightForEthCheck is the minimum chain height before running ETH checks
	MinHeightForEthCheck = 20
	// EthCheckDepth is how many blocks back from head to check
	EthCheckDepth = 30
	// EthCheckBuffer is extra blocks to skip from head to avoid race conditions
	EthCheckBuffer = 5
)

// CheckEthMethods verifies consistency between Ethereum API methods by comparing blocks
// retrieved via eth_getBlockByNumber and eth_getBlockByHash.
// Each node is checked using its own chain head to avoid "future epoch" errors.
func CheckEthMethods(ctx context.Context) error {
	return RetryOperation(ctx, func() error {
		config, err := LoadConfig("/opt/antithesis/resources/config.json")
		if err != nil {
			log.Printf("[ERROR] Failed to load config: %v", err)
			return nil
		}

		filteredNodes := FilterV1Nodes(config.Nodes)
		if len(filteredNodes) == 0 {
			log.Printf("[WARN] No V1 nodes available for ETH check")
			return nil
		}

		for _, node := range filteredNodes {
			log.Printf("[INFO] Checking ETH methods on node %s", node.Name)
			if err := checkEthMethodsOnNode(ctx, node); err != nil {
				log.Printf("[ERROR] ETH check failed on %s: %v", node.Name, err)
			}
		}

		return nil
	}, "ETH methods consistency check")
}

// checkEthMethodsOnNode checks ETH block consistency on a single node
// using that node's own chain head (not a cross-node finalized tipset)
func checkEthMethodsOnNode(ctx context.Context, node NodeConfig) error {
	api, closer, err := ConnectToNode(ctx, node)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer closer()

	// Get THIS node's chain head
	head, err := api.ChainHead(ctx)
	if err != nil {
		return fmt.Errorf("failed to get chain head: %w", err)
	}

	currentHeight := head.Height()
	log.Printf("[INFO] Node %s chain head: %d", node.Name, currentHeight)

	if currentHeight < MinHeightForEthCheck {
		log.Printf("[INFO] Node %s height %d < %d, skipping ETH check", node.Name, currentHeight, MinHeightForEthCheck)
		return nil
	}

	// Start checking from (head - buffer) to avoid race conditions at the tip
	startHeight := currentHeight - abi.ChainEpoch(EthCheckBuffer)
	targetHeight := startHeight - abi.ChainEpoch(EthCheckDepth)
	if targetHeight < 0 {
		targetHeight = 0
	}

	log.Printf("[INFO] Checking heights %d down to %d", startHeight, targetHeight)

	successCount := 0
	errorCount := 0
	nullRounds := 0

	for height := startHeight; height > targetHeight; height-- {
		// Skip if tipset doesn't exist (null round check)
		_, err := api.ChainGetTipSetByHeight(ctx, height, types.EmptyTSK)
		if err != nil {
			nullRounds++
			continue
		}

		hex := fmt.Sprintf("0x%x", height)

		// Get block by number (without transactions for speed)
		ethBlockA, err := api.EthGetBlockByNumber(ctx, hex, false)
		if err != nil {
			log.Printf("[WARN] eth_getBlockByNumber failed at height %d: %v", height, err)
			errorCount++
			continue
		}

		// Get block by hash (also without transactions for fair comparison)
		ethBlockB, err := api.EthGetBlockByHash(ctx, ethBlockA.Hash, false)
		if err != nil {
			log.Printf("[WARN] eth_getBlockByHash failed at height %d: %v", height, err)
			errorCount++
			continue
		}

		// Compare key fields
		hashMatch := ethBlockA.Hash == ethBlockB.Hash
		numberMatch := ethBlockA.Number == ethBlockB.Number
		parentMatch := ethBlockA.ParentHash == ethBlockB.ParentHash
		timestampMatch := ethBlockA.Timestamp == ethBlockB.Timestamp

		allMatch := hashMatch && numberMatch && parentMatch && timestampMatch

		if !allMatch {
			log.Printf("[ERROR] Block mismatch at height %d:", height)
			log.Printf("  Hash match: %v (%s vs %s)", hashMatch, ethBlockA.Hash, ethBlockB.Hash)
			log.Printf("  Number match: %v (%d vs %d)", numberMatch, ethBlockA.Number, ethBlockB.Number)
			log.Printf("  ParentHash match: %v", parentMatch)
			log.Printf("  Timestamp match: %v", timestampMatch)

			AssertAlways(node.Name, false,
				"ETH block consistency: Blocks should be identical regardless of retrieval method",
				map[string]interface{}{
					"height":       height,
					"hash_match":   hashMatch,
					"number_match": numberMatch,
					"parent_match": parentMatch,
					"time_match":   timestampMatch,
				})
			errorCount++
		} else {
			successCount++
		}
	}

	log.Printf("[INFO] ETH check summary for %s: %d OK, %d errors, %d null rounds",
		node.Name, successCount, errorCount, nullRounds)

	return nil
}

// PerformEthMethodsCheck checks ETH methods consistency
func PerformEthMethodsCheck(ctx context.Context) error {
	log.Printf("[INFO] Starting ETH methods consistency check...")
	err := CheckEthMethods(ctx)
	if err != nil {
		log.Printf("[ERROR] ETH methods check failed: %v", err)
		return err
	}
	log.Printf("[INFO] ETH methods consistency check completed")
	return nil
}
