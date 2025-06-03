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

			ethBlockB, err := api.EthGetBlockByHash(ctx, ethBlockA.Hash, false)
			if err != nil {
				fmt.Printf("[FAIL] failed to get tipset @%d via eth_getBlockByHash: %s\n", i, err)
				continue
			}

			// Use DeepEqual to check overall block equality
			equal := reflect.DeepEqual(ethBlockA, ethBlockB)
			assert.Always(equal, "Blocks should be identical regardless of retrieval method", map[string]interface{}{
				"height":        i,
				"node":          node.Name,
				"blockByNumber": ethBlockA,
				"blockByHash":   ethBlockB,
			})

			// Additional specific field checks for better error reporting
			assert.Always(ethBlockA.Hash == ethBlockB.Hash, "Block hashes should be identical", map[string]interface{}{
				"height":        i,
				"node":          node.Name,
				"blockByNumber": ethBlockA.Hash,
				"blockByHash":   ethBlockB.Hash,
			})

			assert.Always(ethBlockA.Number == ethBlockB.Number, "Block numbers should be identical", map[string]interface{}{
				"height":        i,
				"node":          node.Name,
				"blockByNumber": ethBlockA.Number,
				"blockByHash":   ethBlockB.Number,
			})

			assert.Always(ethBlockA.ParentHash == ethBlockB.ParentHash, "Parent hashes should be identical", map[string]interface{}{
				"height":        i,
				"node":          node.Name,
				"blockByNumber": ethBlockA.ParentHash,
				"blockByHash":   ethBlockB.ParentHash,
			})

			assert.Always(ethBlockA.Timestamp == ethBlockB.Timestamp, "Block timestamps should be identical", map[string]interface{}{
				"height":        i,
				"node":          node.Name,
				"blockByNumber": ethBlockA.Timestamp,
				"blockByHash":   ethBlockB.Timestamp,
			})

			if equal {
				fmt.Printf("[OK] blocks received via eth_getBlockByNumber and eth_getBlockByHash for tipset @%d are identical\n", i)
			}
		}
	}
	return nil
}
