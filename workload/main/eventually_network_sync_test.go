package main

import (
	"context"
	"testing"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/stretchr/testify/assert"
)

func TestChainSynchronization(t *testing.T) {

	ctx := context.Background()

	time.Sleep(15 * time.Second)

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.NoError(t, err, "Failed to load config")

	// Ensure there are nodes in the configuration
	if len(config.Nodes) == 0 {
		t.Fatal("No nodes found in config.json")
	}

	// Connect to all nodes and record their initial heights
	var initialHeights []int
	for _, node := range config.Nodes {
		t.Run(node.Name, func(t *testing.T) {
			api, closer, err := resources.ConnectToNode(ctx, node)
			assert.NoError(t, err, "Failed to connect to Lotus node")
			defer closer()

			// Get initial chain height
			head, err := api.ChainHead(ctx)
			assert.NoError(t, err, "Failed to get chain head")
			initialHeights = append(initialHeights, int(head.Height()))
		})
	}

	// Wait and check if all nodes reach the same chain height
	timeout := 60 * time.Second
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-time.After(timeout):
			t.Fatalf("Nodes did not synchronize within %s", timeout)
		case <-ticker.C:
			var currentHeights []int
			allMatch := true

			for _, node := range config.Nodes {
				t.Run(node.Name, func(t *testing.T) {
					api, closer, err := resources.ConnectToNode(ctx, node)
					assert.NoError(t, err, "Failed to connect to Lotus node")
					defer closer()

					head, err := api.ChainHead(ctx)
					assert.NoError(t, err, "Failed to get chain head")
					currentHeights = append(currentHeights, int(head.Height()))
				})
			}

			for i := 1; i < len(currentHeights); i++ {
				if currentHeights[i] != currentHeights[0] {
					allMatch = false
					break
				}
			}

			if allMatch {
				t.Logf("All nodes synchronized at height %d", currentHeights[0])
				return
			}
		}
	}
}
