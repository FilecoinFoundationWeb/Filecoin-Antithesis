package main

import (
	"context"
	"sync"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

func TestF3IsRunningEquality(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.Always(err == nil, "Loading the resources config", map[string]interface{}{"error": err})

	nodeNames := []string{"Lotus1", "Lotus2", "Forest"}
	var filterNodes []resources.NodeConfig

	// Filter nodes
	for _, node := range config.Nodes {
		for _, name := range nodeNames {
			if node.Name == name {
				filterNodes = append(filterNodes, node)
			}
		}
	}

	var wg sync.WaitGroup

	for _, node := range filterNodes {
		wg.Add(1)
		go func(node resources.NodeConfig) {
			defer wg.Done()

			api, closer, err := resources.ConnectToNode(ctx, node)
			assert.Always(err == nil, "Connecting to a node", map[string]interface{}{"node": node.Name, "error": err})

			if err != nil {
				return
			}
			defer closer()

			isRunning, err := api.F3IsRunning(ctx)
			assert.Always(err == nil, "Fetching F3 running status", map[string]interface{}{"node": node.Name, "error": err})

			if err != nil {
				t.Logf("Error fetching F3 status for node: %v", node.Name)
				return
			}

			if isRunning {
				assert.Sometimes(isRunning, "F3 is running on node", map[string]interface{}{"node": node.Name})
			} else {
				t.Logf("Node: %v is not running F3", node.Name)
			}
		}(node)
	}

	// Wait for all goroutines to complete
	wg.Wait()
}
