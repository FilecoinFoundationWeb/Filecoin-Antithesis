package main

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
)

// Helper to get a few random nodes from the config
func getRandomNodes(config *resources.Config, count int) []resources.NodeConfig {
	var lotusNodes []resources.NodeConfig
	for _, node := range config.Nodes {
		if node.Name == "Lotus1" || node.Name == "Lotus2" {
			lotusNodes = append(lotusNodes, node)
		}
	}

	if count >= len(lotusNodes) {
		return lotusNodes
	}
	return lotusNodes[:count]
}

// --- Test: Chain Weight ---
func TestChainWeightIncreasing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	nodesToCheck := getRandomNodes(config, 3)
	var wg sync.WaitGroup
	errChan := make(chan error, len(nodesToCheck))

	nodeWeights := make(map[string]types.BigInt)
	for _, nodeCfg := range nodesToCheck {
		api, closer, err := resources.ConnectToNode(ctx, nodeCfg)
		if err != nil {
			t.Fatalf("Failed to connect to node: %v", err)
		}

		head, err := api.ChainHead(ctx)
		if err != nil {
			t.Fatalf("Failed to get chain head: %v", err)
		}

		nodeWeights[nodeCfg.Name] = head.ParentWeight()
		closer()
		t.Logf("Initial weight for node %s: %s", nodeCfg.Name, head.ParentWeight().String())
	}

	time.Sleep(30 * time.Second)

	// Now check if weights have increased
	for _, nodeCfg := range nodesToCheck {
		wg.Add(1)
		go func(nodeCfg resources.NodeConfig) {
			defer wg.Done()

			api, closer, err := resources.ConnectToNode(ctx, nodeCfg)
			if err != nil {
				errChan <- fmt.Errorf("failed to connect to node %s: %v", nodeCfg.Name, err)
				return
			}
			defer closer()

			head, err := api.ChainHead(ctx)
			if err != nil {
				errChan <- fmt.Errorf("failed to get chain head for node %s: %v", nodeCfg.Name, err)
				return
			}

			currentWeight := head.ParentWeight()
			initialWeight := nodeWeights[nodeCfg.Name]

			assert.Always(currentWeight.GreaterThanEqual(initialWeight), "Chain weight should be non-decreasing",
				map[string]interface{}{
					"node":           nodeCfg.Name,
					"current_height": head.Height(),
					"current_weight": currentWeight.String(),
					"initial_weight": initialWeight.String(),
				})

			t.Logf("Node %s: Head Height: %d, Initial Weight: %s, Current Weight: %s",
				nodeCfg.Name, head.Height(), initialWeight.String(), currentWeight.String())
		}(nodeCfg)
	}
	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Error(err)
	}
}

// --- Test: Parent-Child Relationships ---
func TestParentChildRelationships(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	nodesToCheck := getRandomNodes(config, 1)
	if len(nodesToCheck) == 0 {
		t.Skip("No nodes to check for parent-child relationships")
		return
	}
	nodeCfg := nodesToCheck[0]

	api, closer, err := resources.ConnectToNode(ctx, nodeCfg)
	if err != nil {
		t.Fatalf("Failed to connect to node: %v", err)
	}
	defer closer()

	head, err := api.ChainHead(ctx)
	if err != nil {
		t.Fatalf("Failed to get chain head: %v", err)
	}

	currentTS := head
	for i := 0; i < 5 && currentTS != nil && currentTS.Height() > 0; i++ {
		assert.Always(len(currentTS.Blocks()) > 0, "Tipset must have blocks",
			map[string]interface{}{"node": nodeCfg.Name, "height": currentTS.Height(), "tipset_key": currentTS.Key().String()})

		block := currentTS.Blocks()[0]
		assert.Always(block.Height == currentTS.Height(), "Block height must match tipset height",
			map[string]interface{}{"node": nodeCfg.Name, "block_height": block.Height, "tipset_height": currentTS.Height()})

		if block.Height > 0 {
			assert.Always(len(block.Parents) > 0, "Non-genesis block must have parents",
				map[string]interface{}{"node": nodeCfg.Name, "height": block.Height})

			parentTS, err := api.ChainGetTipSet(ctx, types.NewTipSetKey(block.Parents...))
			assert.Always(err == nil, "Fetching parent tipset",
				map[string]interface{}{"node": nodeCfg.Name, "child_height": block.Height, "parent_cids": block.Parents, "error": err})

			assert.Always(parentTS.Height() == block.Height-1, "Parent tipset height should be child height - 1",
				map[string]interface{}{
					"node":                 nodeCfg.Name,
					"child_height":         block.Height,
					"parent_tipset_height": parentTS.Height(),
				})

			t.Logf("Node %s: Height %d, Parent Height %d - OK", nodeCfg.Name, block.Height, parentTS.Height())
			currentTS = parentTS
		} else {
			t.Logf("Node %s: Reached Genesis at Height %d", nodeCfg.Name, block.Height)
			break
		}
	}
}

var (
	lastHeadTime   time.Time
	lastHeadHeight abi.ChainEpoch
	blockRateMu    sync.Mutex
)

const expectedBlockTime = 8 * time.Second

func TestBlockProductionRate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}
	nodesToCheck := getRandomNodes(config, 1)
	if len(nodesToCheck) == 0 {
		t.Skip("No nodes to check for block production rate")
		return
	}
	nodeCfg := nodesToCheck[0]

	api, closer, err := resources.ConnectToNode(ctx, nodeCfg)
	if err != nil {
		t.Fatalf("Failed to connect to node: %v", err)
	}
	defer closer()

	head, err := api.ChainHead(ctx)
	if err != nil {
		t.Fatalf("Failed to get chain head: %v", err)
	}

	blockRateMu.Lock()
	defer blockRateMu.Unlock()

	if !lastHeadTime.IsZero() && head.Height() > lastHeadHeight {
		elapsed := time.Since(lastHeadTime)
		epochsProduced := head.Height() - lastHeadHeight
		avgTimePerEpoch := elapsed / time.Duration(epochsProduced)

		maxAllowedTime := expectedBlockTime * 2
		minAllowedTime := expectedBlockTime / 2

		assert.Sometimes(avgTimePerEpoch <= maxAllowedTime, "Average block time not excessively long",
			map[string]interface{}{
				"node":               nodeCfg.Name,
				"avg_time_s":         avgTimePerEpoch.Seconds(),
				"epochs_produced":    epochsProduced,
				"expected_time_s":    expectedBlockTime.Seconds(),
				"max_allowed_time_s": maxAllowedTime.Seconds(),
			})

		assert.Sometimes(avgTimePerEpoch >= minAllowedTime, "Average block time not excessively short",
			map[string]interface{}{
				"node":               nodeCfg.Name,
				"avg_time_s":         avgTimePerEpoch.Seconds(),
				"epochs_produced":    epochsProduced,
				"expected_time_s":    expectedBlockTime.Seconds(),
				"min_allowed_time_s": minAllowedTime.Seconds(),
			})

		t.Logf("Node %s: Avg block time for last %d epochs: %v", nodeCfg.Name, epochsProduced, avgTimePerEpoch)
	}

	lastHeadTime = time.Now()
	lastHeadHeight = head.Height()
}
