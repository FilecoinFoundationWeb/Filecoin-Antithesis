package main

import (
	"context"
	"testing"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

func TestNodeHeightProgression(t *testing.T) {
	ctx := context.Background()

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
		t.Logf("Node '%s' initial chain height: %d", node.Name, initialHeight)

		// Wait for 30 seconds and check if height increased
		time.Sleep(30 * time.Second)

		currentHead, err := api.ChainHead(ctx)
		if err != nil {
			t.Logf("Failed to get current head for node '%s': %v", node.Name, err)
			continue
		}

		currentHeight := int64(currentHead.Height())
		t.Logf("Node '%s' final chain height: %d (change: %d)",
			node.Name, currentHeight, currentHeight-initialHeight)

		// Simple assertion that height should increase
		assert.Sometimes(currentHeight > initialHeight, "Chain height progression", map[string]interface{}{
			"node":           node.Name,
			"initial_height": initialHeight,
			"final_height":   currentHeight,
			"change":         currentHeight - initialHeight,
			"property":       "chain height should increase over time",
		})
	}
}
