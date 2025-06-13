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
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

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

	for _, node := range filteredNodes {
		t.Run(node.Name, func(t *testing.T) {
			api, closer, err := resources.ConnectToNode(ctx, node)
			if err != nil {
				t.Fatalf("Failed to connect to node: %v", err)
			}
			defer closer()

			// Get chain head with proper error handling
			head, err := api.ChainHead(ctx)
			if err != nil {
				t.Fatalf("Failed to get chain head: %v", err)
			}

			height := head.Height()
			if height <= 1 {
				t.Logf("Chain height too low for backfill test: %d", height)
				return
			}

			// Test backfill with the previous height
			_, err = api.ChainValidateIndex(ctx, height-1, true)
			assert.Sometimes(err == nil,
				"[Chain Validation] Chain index validation should succeed",
				resources.EnhanceAssertDetails(
					map[string]interface{}{
						"error":    err,
						"height":   height - 1,
						"property": "Chain index validation",
						"impact":   "High - validates chain index consistency",
						"details":  "Chain index validation ensures proper chain state tracking",
					},
					node.Name,
				))
		})
	}
}
