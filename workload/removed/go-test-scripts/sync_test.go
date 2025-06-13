package main

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/lotus/build/buildconstants"
)

func TestNodeHeightProgression(t *testing.T) {
	ctx := context.Background()
	blocktime := uint64(5)
	expectedBlockTime := float64(blocktime)      // seconds
	allowedDeviation := float64(blocktime) * 0.3 // 30% deviation allowed

	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	if err != nil {
		t.Skip("Skipping test: failed to load config")
	}

	nodeNames := []string{"Lotus1", "Lotus2"}
	var filteredNodes []resources.NodeConfig
	for _, node := range config.Nodes {
		for _, name := range nodeNames {
			if node.Name == name {
				filteredNodes = append(filteredNodes, node)
			}
		}
	}

	for _, node := range filteredNodes {
		api, closer, err := resources.ConnectToNode(ctx, node)
		if err != nil {
			t.Skip("Skipping test: could not connect to node")
		}
		defer closer()

		initialHead, err := api.ChainHead(ctx)
		if err != nil {
			t.Skip("Skipping test: could not get initial chain head")
		}

		initialHeight := int64(initialHead.Height())
		startTime := time.Now()
		t.Logf("Node '%s' initial chain height: %d", node.Name, initialHeight)

		// Wait for 30 seconds and check if height increased
		time.Sleep(30 * time.Second)
		elapsedTime := time.Since(startTime).Seconds()

		currentHead, err := api.ChainHead(ctx)
		if err != nil {
			t.Logf("Failed to get current head for node '%s': %v", node.Name, err)
			continue
		}

		currentHeight := int64(currentHead.Height())
		heightDiff := currentHeight - initialHeight
		t.Logf("Node '%s' final chain height: %d (change: %d)",
			node.Name, currentHeight, heightDiff)

		// Calculate actual block time
		actualBlockTime := elapsedTime / float64(heightDiff)
		t.Logf("Node '%s' average block time: %.2f seconds (expected: %.2f ± %.2f)",
			node.Name, actualBlockTime, expectedBlockTime, allowedDeviation)

		// Assert that height increased
		assert.Always(currentHeight > initialHeight,
			"[Chain Growth] Chain height should increase over time",
			resources.EnhanceAssertDetails(
				map[string]interface{}{
					"node":           node.Name,
					"initial_height": initialHeight,
					"final_height":   currentHeight,
					"change":         heightDiff,
					"elapsed_time":   elapsedTime,
					"property":       "Chain height progression",
					"impact":         "Critical - validates chain liveness",
					"details":        "Chain height must increase to demonstrate active block production",
				},
				node.Name,
			))

		// Assert that block time is within expected range
		blockTimeWithinRange := math.Abs(actualBlockTime-expectedBlockTime) <= allowedDeviation
		assert.Sometimes(blockTimeWithinRange,
			"[Block Time] Block production time should be within expected range",
			resources.EnhanceAssertDetails(
				map[string]interface{}{
					"node":              node.Name,
					"actual_block_time": actualBlockTime,
					"expected":          expectedBlockTime,
					"deviation":         math.Abs(actualBlockTime - expectedBlockTime),
					"max_allowed_dev":   allowedDeviation,
					"property":          "Block time consistency",
					"impact":            "High - indicates network health",
					"details":           fmt.Sprintf("Block time should be approximately %d seconds", buildconstants.BlockDelaySecs),
					"recommendation":    "If failing, investigate network conditions and node performance",
				},
				node.Name,
			))
	}
}
