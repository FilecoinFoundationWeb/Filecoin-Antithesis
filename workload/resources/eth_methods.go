package resources

import (
	"context"
	"fmt"
	"reflect"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
)

func CheckEthMethods(ctx context.Context) error {
	config, err := LoadConfig("/opt/antithesis/resources/config.json")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	nodeNames := []string{"Lotus1", "Lotus2"}

	var filteredNodes []NodeConfig
	for _, node := range config.Nodes {
		for _, name := range nodeNames {
			if node.Name == name {
				filteredNodes = append(filteredNodes, node)
			}
		}
	}

	for _, node := range filteredNodes {
		api, closer, err := ConnectToNode(ctx, node)
		if err != nil {
			return fmt.Errorf("failed to connect to node %s: %w", node.Name, err)
		}
		defer closer()

		head, err := api.ChainHead(ctx)
		if err != nil {
			return fmt.Errorf("failed to get chain head: %w", err)
		}

		height := head.Height()
		targetHeight := height - 30
		for i := int64(height); i > int64(targetHeight); i-- {
			if _, err := api.ChainGetTipSetByHeight(ctx, abi.ChainEpoch(i), types.EmptyTSK); err != nil {
				fmt.Printf("[FAIL] failed to get tipset @%d from Lotus: %s\n", i, err)
				continue
			}

			hex := fmt.Sprintf("0x%x", i)
			ethBlockA, err := api.EthGetBlockByNumber(ctx, hex, false)
			if err != nil {
				fmt.Printf("[FAIL] failed to get tipset @%d via eth_getBlockByNumber: %s\n", i, err)
				continue
			}
			fmt.Printf("[DEBUG] Block by Number - Height: %d, Hash: %s\n", i, ethBlockA.Hash)

			ethBlockB, err := api.EthGetBlockByHash(ctx, ethBlockA.Hash, false)
			if err != nil {
				fmt.Printf("[FAIL] failed to get tipset @%d via eth_getBlockByHash: %s\n", i, err)
				continue
			}
			fmt.Printf("[DEBUG] Block by Hash - Height: %d, Hash: %s\n", i, ethBlockB.Hash)

			// Use DeepEqual to check overall block equality
			equal := reflect.DeepEqual(ethBlockA, ethBlockB)
			if !equal {
				fmt.Printf("[WARN] Block mismatch at height %d:\n", i)
				fmt.Printf("  Block by Number Hash: %s\n", ethBlockA.Hash)
				fmt.Printf("  Block by Hash Hash: %s\n", ethBlockB.Hash)
				fmt.Printf("  Block by Number ParentHash: %s\n", ethBlockA.ParentHash)
				fmt.Printf("  Block by Hash ParentHash: %s\n", ethBlockB.ParentHash)
				fmt.Printf("  Block by Number Number: %s\n", ethBlockA.Number)
				fmt.Printf("  Block by Hash Number: %s\n", ethBlockB.Number)
				fmt.Printf("  Block by Number Timestamp: %s\n", ethBlockA.Timestamp)
				fmt.Printf("  Block by Hash Timestamp: %s\n", ethBlockB.Timestamp)
			}

			assert.Always(equal,
				"[Block Consistency] Blocks should be identical regardless of retrieval method",
				EnhanceAssertDetails(
					map[string]interface{}{
						"height":         i,
						"node":           node.Name,
						"blockByNumber":  ethBlockA,
						"blockByHash":    ethBlockB,
						"property":       "Block data consistency",
						"impact":         "Critical - indicates API inconsistency",
						"details":        "Block data must be identical when retrieved by number or hash",
						"recommendation": "Check block retrieval and serialization logic",
					},
					"eth_methods",
				))

			// Additional specific field checks for better error reporting
			assert.Always(ethBlockA.Hash == ethBlockB.Hash,
				"[Block Hash] Block hashes must be identical",
				EnhanceAssertDetails(
					map[string]interface{}{
						"height":        i,
						"node":          node.Name,
						"blockByNumber": ethBlockA.Hash,
						"blockByHash":   ethBlockB.Hash,
						"property":      "Block hash consistency",
						"impact":        "Critical - indicates hash computation error",
						"details":       "Block hash must be identical across retrieval methods",
					},
					"eth_methods",
				))

			assert.Always(ethBlockA.Number == ethBlockB.Number,
				"[Block Number] Block numbers must be identical",
				EnhanceAssertDetails(
					map[string]interface{}{
						"height":        i,
						"node":          node.Name,
						"blockByNumber": ethBlockA.Number,
						"blockByHash":   ethBlockB.Number,
						"property":      "Block number consistency",
						"impact":        "Critical - indicates block height mismatch",
						"details":       "Block number must be identical across retrieval methods",
					},
					"eth_methods",
				))

			assert.Always(ethBlockA.ParentHash == ethBlockB.ParentHash,
				"[Parent Hash] Parent hashes must be identical",
				EnhanceAssertDetails(
					map[string]interface{}{
						"height":        i,
						"node":          node.Name,
						"blockByNumber": ethBlockA.ParentHash,
						"blockByHash":   ethBlockB.ParentHash,
						"property":      "Parent hash consistency",
						"impact":        "Critical - indicates chain linking error",
						"details":       "Parent hash must be identical across retrieval methods",
					},
					"eth_methods",
				))

			assert.Always(ethBlockA.Timestamp == ethBlockB.Timestamp,
				"[Block Timestamp] Block timestamps must be identical",
				EnhanceAssertDetails(
					map[string]interface{}{
						"height":        i,
						"node":          node.Name,
						"blockByNumber": ethBlockA.Timestamp,
						"blockByHash":   ethBlockB.Timestamp,
						"property":      "Block timestamp consistency",
						"impact":        "Critical - indicates timestamp mismatch",
						"details":       "Block timestamp must be identical across retrieval methods",
					},
					"eth_methods",
				))

			if equal {
				fmt.Printf("[OK] blocks received via eth_getBlockByNumber and eth_getBlockByHash for tipset @%d are identical\n", i)
			}
		}
	}
	return nil
}
