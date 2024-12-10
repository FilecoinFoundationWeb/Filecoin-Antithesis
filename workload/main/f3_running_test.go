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
	assert.Always(err == nil, "Failed to load config: %v", map[string]interface{}{"error": err})

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
	var mu sync.Mutex
	errors := make(map[string]interface{})
	results := make(map[string]bool)

	for _, node := range filterNodes {
		wg.Add(1)
		go func(node resources.NodeConfig) {
			defer wg.Done()

			api, closer, err := resources.ConnectToNode(ctx, node)
			if err != nil {
				mu.Lock()
				errors[node.Name] = map[string]interface{}{"error": err, "message": "Failed to connect to node"}
				mu.Unlock()
				return
			}
			defer closer()

			isRunning, err := api.F3IsRunning(ctx)
			if err != nil {
				mu.Lock()
				errors[node.Name] = map[string]interface{}{"error": err, "message": "Failed to fetch F3 running status"}
				mu.Unlock()
				return
			}

			mu.Lock()
			results[node.Name] = isRunning
			mu.Unlock()

			if !isRunning {
				t.Logf("Node: %v not running F3", node.Name)
			}
		}(node)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Handle errors
	for node, err := range errors {
		assert.Always(false, "Node '%s' encountered an error: %v", map[string]interface{}{
			"node":  node,
			"error": err,
		})
	}

	// Validate results
	for node, isRunning := range results {
		assert.Always(isRunning, "F3 is not running on node: %s", map[string]interface{}{"node": node})
	}
}
