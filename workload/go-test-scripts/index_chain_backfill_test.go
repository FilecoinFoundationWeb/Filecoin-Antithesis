package main

import (
	"context"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

func TestBackfill(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.Always(err == nil, "Loading the resources config", map[string]interface{}{"error": err})

	// Hardcoded list of Lotus nodes to test
	nodeNames := []string{"Lotus1", "Lotus2"}

	// Filter the nodes based on the specified node names
	var filteredNodes []resources.NodeConfig
	for _, node := range config.Nodes {
		for _, name := range nodeNames {
			if node.Name == name {
				filteredNodes = append(filteredNodes, node)
			}
		}
	}

	// Ensure we found at least one node
	assert.Always(len(filteredNodes) > 0, "Found at least one node to test",
		map[string]interface{}{"nodeCount": len(filteredNodes), "requestedNodes": nodeNames})

	for _, node := range filteredNodes {
		t.Run(node.Name, func(t *testing.T) {
			api, closer, err := resources.ConnectToNode(ctx, node)
			assert.Always(err == nil, "Connecting to node",
				map[string]interface{}{"node": node.Name, "error": err})
			if err != nil {
				return
			}
			defer closer()

			// Get chain head with proper error handling
			head, err := api.ChainHead(ctx)
			assert.Always(err == nil, "Getting chain head",
				map[string]interface{}{"node": node.Name, "error": err})
			if err != nil || head == nil {
				return
			}

			height := head.Height()
			if height <= 1 {
				t.Logf("Chain height too low for backfill test: %d", height)
				return
			}

			// Test backfill with the previous height
			_, err = api.ChainValidateIndex(ctx, height-1, true)
			assert.Sometimes(err == nil, "ChainValidateIndex call successful",
				map[string]interface{}{
					"node":   node.Name,
					"error":  err,
					"height": height - 1,
				})
		})
	}
}
