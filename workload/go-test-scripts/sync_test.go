package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

func TestNodeHeightProgression(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.Always(err == nil, "Loading the resources config", map[string]interface{}{"error": err})

	// Hardcoded list of Lotus nodes to test
	nodeNames := []string{"Lotus1", "Lotus2"}

	//DELETE
	fmt.Print(nodeNames)

	// Filter the nodes based on the specified node names
	var filteredNodes []resources.NodeConfig
	for _, node := range config.Nodes {
		for _, name := range nodeNames {
			if node.Name == name {
				filteredNodes = append(filteredNodes, node)
			}
		}
	}

	//DELETE
	fmt.Print(filteredNodes)

	// Test chain height progression for each filtered node
	for _, node := range filteredNodes {
		api, closer, err := resources.ConnectToNode(ctx, node)
		assert.Always(err == nil, "Connecting to a node", map[string]interface{}{"node": node, "error": err})

		if err != nil {
			return
		}
		defer closer()

		// Get initial chain height
		initialHead, err := api.ChainHead(ctx)
		assert.Always(err == nil, "Getting the chainhead for a node", map[string]interface{}{"node": node, "error": err})

		if err != nil {
			return
		}

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
				assert.Always(false, "Chain height for a node progresses when checked", map[string]interface{}{
					"node":           node,
					"initial_height": initialHeight,
					"error":          nil,
				})
			case <-ticker.C:
				currentHead, err := api.ChainHead(ctx)
				assert.Always(err == nil, "Getting the chainhead for a node", map[string]interface{}{"node": node, "error": err})

				if err != nil {
					return
				}

				currentHeight := int(currentHead.Height())

				if currentHeight > initialHeight {

					assert.Always(true, "Chain height for a node progresses when checked", map[string]interface{}{
						"node":           node,
						"initial_height": initialHeight,
						"current_height": currentHeight,
						"error":          nil,
					})
					t.Logf("Node '%s' chain height progressed: initial=%d, current=%d", node.Name, initialHeight, currentHeight)
					progressed = true
				}
			}
			if progressed {
				break
			}
		}
	}
}
