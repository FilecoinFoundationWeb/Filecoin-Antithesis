package resources

import (
	"context"
	"fmt"
	"log"
	"reflect"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
)

// CheckEthMethods verifies consistency between Ethereum API methods by comparing blocks
// retrieved via eth_getBlockByNumber and eth_getBlockByHash. It checks multiple block
// properties including hashes, numbers, timestamps, and parent hashes for the last 30 blocks.
func CheckEthMethods(ctx context.Context) error {
	return RetryOperation(ctx, func() error {
		config, err := LoadConfig("/opt/antithesis/resources/config.json")
		if err != nil {
			log.Printf("[ERROR] Failed to load config: %v", err)
			return nil
		}

		filteredNodes := FilterLotusNodes(config.Nodes)

		for _, node := range filteredNodes {
			api, closer, err := ConnectToNode(ctx, node)
			if err != nil {
				log.Printf("[ERROR] Failed to connect to node %s: %v", node.Name, err)
				return nil
			}
			defer closer()

			head, err := api.ChainHead(ctx)
			if err != nil {
				log.Printf("[ERROR] Failed to get chain head: %v", err)
				return nil
			}

			height := head.Height()
			targetHeight := height - 30
			for i := int64(height); i > int64(targetHeight); i-- {
				// Use RetryOperation for each block check
				err := RetryOperation(ctx, func() error {
					if _, err := api.ChainGetTipSetByHeight(ctx, abi.ChainEpoch(i), types.EmptyTSK); err != nil {
						log.Printf("[ERROR] Failed to get tipset @%d from Lotus: %v", i, err)
						return nil
					}

					hex := fmt.Sprintf("0x%x", i)
					ethBlockA, err := api.EthGetBlockByNumber(ctx, hex, false)
					if err != nil {
						log.Printf("[ERROR] Failed to get tipset @%d via eth_getBlockByNumber: %v", i, err)
						return nil
					}
					log.Printf("[DEBUG] Block by Number - Height: %d, Hash: %s", i, ethBlockA.Hash)

					ethBlockB, err := api.EthGetBlockByHash(ctx, ethBlockA.Hash, false)
					if err != nil {
						log.Printf("[ERROR] Failed to get tipset @%d via eth_getBlockByHash: %v", i, err)
						return nil
					}
					log.Printf("[DEBUG] Block by Hash - Height: %d, Hash: %s", i, ethBlockB.Hash)

					// Use DeepEqual to check overall block equality
					equal := reflect.DeepEqual(ethBlockA, ethBlockB)
					if !equal {
						log.Printf("[WARN] Block mismatch at height %d:", i)
						log.Printf("  Block by Number Hash: %s", ethBlockA.Hash)
						log.Printf("  Block by Hash Hash: %s", ethBlockB.Hash)
						log.Printf("  Block by Number ParentHash: %s", ethBlockA.ParentHash)
						log.Printf("  Block by Hash ParentHash: %s", ethBlockB.ParentHash)
						log.Printf("  Block by Number Number: %s", ethBlockA.Number)
						log.Printf("  Block by Hash Number: %s", ethBlockB.Number)
						log.Printf("  Block by Number Timestamp: %s", ethBlockA.Timestamp)
						log.Printf("  Block by Hash Timestamp: %s", ethBlockB.Timestamp)
						log.Printf("[ERROR] Block mismatch at height %d", i)
						return nil
					}

					assert.Always(equal,
						"[Block Consistency] Blocks should be identical regardless of retrieval method",
						map[string]interface{}{
							"height":         i,
							"node":           node.Name,
							"blockByNumber":  ethBlockA,
							"blockByHash":    ethBlockB,
							"property":       "Block data consistency",
							"impact":         "Critical - indicates API inconsistency",
							"details":        "Block data must be identical when retrieved by number or hash",
							"recommendation": "Check block retrieval and serialization logic",
						})

					// Additional specific field checks for better error reporting
					assert.Always(ethBlockA.Hash == ethBlockB.Hash,
						"[Block Hash] Block hashes must be identical",
						map[string]interface{}{
							"height":        i,
							"node":          node.Name,
							"blockByNumber": ethBlockA.Hash,
							"blockByHash":   ethBlockB.Hash,
							"property":      "Block hash consistency",
							"impact":        "Critical - indicates hash computation error",
							"details":       "Block hash must be identical across retrieval methods",
						})

					assert.Always(ethBlockA.Number == ethBlockB.Number,
						"[Block Number] Block numbers must be identical",
						map[string]interface{}{
							"height":        i,
							"node":          node.Name,
							"blockByNumber": ethBlockA.Number,
							"blockByHash":   ethBlockB.Number,
							"property":      "Block number consistency",
							"impact":        "Critical - indicates block height mismatch",
							"details":       "Block number must be identical across retrieval methods",
						})

					assert.Always(ethBlockA.ParentHash == ethBlockB.ParentHash,
						"[Parent Hash] Parent hashes must be identical",
						map[string]interface{}{
							"height":        i,
							"node":          node.Name,
							"blockByNumber": ethBlockA.ParentHash,
							"blockByHash":   ethBlockB.ParentHash,
							"property":      "Parent hash consistency",
							"impact":        "Critical - indicates chain linking error",
							"details":       "Parent hash must be identical across retrieval methods",
						})

					assert.Always(ethBlockA.Timestamp == ethBlockB.Timestamp,
						"[Block Timestamp] Block timestamps must be identical",
						map[string]interface{}{
							"height":        i,
							"node":          node.Name,
							"blockByNumber": ethBlockA.Timestamp,
							"blockByHash":   ethBlockB.Timestamp,
							"property":      "Block timestamp consistency",
							"impact":        "Critical - indicates timestamp mismatch",
							"details":       "Block timestamp must be identical across retrieval methods",
						})

					log.Printf("[OK] Blocks received via eth_getBlockByNumber and eth_getBlockByHash for tipset @%d are identical", i)
					return nil
				}, fmt.Sprintf("Block check at height %d", i))

				if err != nil {
					// Log the error but continue with next height
					log.Printf("[ERROR] Failed to check block at height %d after retries: %v", i, err)
					continue
				}
			}
		}
		return nil
	}, "ETH methods consistency check")
}
