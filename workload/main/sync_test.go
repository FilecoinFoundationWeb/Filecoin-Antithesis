package main

import (
	"context"
	"testing"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/stretchr/testify/assert"
)

func TestNodeHeightProgression(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.NoError(t, err, "Failed to load config")

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

	// Test chain height progression for each filtered node
	for _, node := range filteredNodes {
		api, closer, err := resources.ConnectToNode(ctx, node)
		defer closer()
		assert.NoError(t, err, "Failed to connect to Lotus node")

		// Get initial chain height
		initialHead, err := api.ChainHead(ctx)
		assert.NoError(t, err, "Failed to get initial chain head")
		initialHeight := int(initialHead.Height())
		t.Logf("Node '%s' initial chain height: %d", node.Name, initialHeight)

		// Wait for chain height progression
		timeout := 30 * time.Second
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		progressed := false
		for {
			select {
			case <-time.After(timeout):
				t.Fatalf("Node '%s' chain height did not progress within %s", node.Name, timeout)
			case <-ticker.C:
				currentHead, err := api.ChainHead(ctx)
				assert.NoError(t, err, "Failed to get current chain head")
				currentHeight := int(currentHead.Height())

				if currentHeight > initialHeight {
					t.Logf("Node '%s' chain height progressed: initial=%d, current=%d", node.Name, initialHeight, currentHeight)
					progressed = true
				}
			}
			if progressed {
				break
			}
		}

		assert.True(t, progressed, "Node '%s' chain height did not progress", node.Name)
	}
}
