package main

import (
	"context"
	"fmt"
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
	results := make(map[string]bool)

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
			assert.Sometimes(err == nil, "Fetching F3 running status", map[string]interface{}{"node": node.Name, "error": err})

			if err != nil {
				return
			}

			results[node.Name] = isRunning

			if !isRunning {
				t.Logf("Node: %v not running F3", node.Name)
			}
		}(node)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Validate results
	for node, isRunning := range results {
		fmt.Print(node)
		if node == "Lotus1" {
			assert.Sometimes(isRunning, "F3 is running on lotus node 1", map[string]interface{}{"node": node})
		} else if node == "Lotus2" {
			assert.Sometimes(isRunning, "F3 is running on lotus node 2", map[string]interface{}{"node": node})
		} else if node == "Forest" {
			assert.Sometimes(isRunning, "F3 is running on forest", map[string]interface{}{"node": node})
		}
	}
}
