package main

import (
	"context"
	"sync"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

func TestF3GetProgressEquality(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.Always(err == nil, "Workload: Loading the resources config", map[string]interface{}{"error": err})

	nodeNames := []string{"Lotus1", "Lotus2"}
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
	progresses := make(map[string]interface{})

	// Fetch progresses concurrently
	for _, node := range filterNodes {
		wg.Add(1)
		go func(node resources.NodeConfig) {
			defer wg.Done()

			api, closer, err := resources.ConnectToNode(ctx, node)
			if node.Name == "Forest" {
				assert.Always(err == nil, "Forest: Successful http jsonrpc client connection", map[string]interface{}{"node": node.Name, "error": err})
			} else if node.Name == "Lotus1" || node.Name == "Lotus2" {
				assert.Always(err == nil, "Lotus: Successful http jsonrpc client connection", map[string]interface{}{"node": node.Name, "error": err})
			}

			if err != nil {
				return
			}
			defer closer()

			isRunning, err := api.F3IsRunning(ctx)
			if node.Name == "Forest" {
				assert.Always(err == nil, "Forest: Fetching F3 running status", map[string]interface{}{"node": node.Name, "error": err})
			} else if node.Name == "Lotus1" || node.Name == "Lotus2" {
				assert.Always(err == nil, "Lotus: Fetching F3 running status", map[string]interface{}{"node": node.Name, "error": err})
			}

			if !isRunning {
				return
			}

			progress, err := api.F3GetProgress(ctx)
			assert.Always(err == nil, "Fetching F3 progress from a node while F3 is active", map[string]interface{}{"node": node.Name, "error": err})

			if err != nil {
				progresses[node.Name] = nil
				return
			} else {
				progresses[node.Name] = progress
			}
		}(node)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Check that we have all progresses for the nodes
	for _, progress := range progresses {
		if progress == nil {
			return
		}
	}

	// Assert all progresses are identical
	var reference interface{}
	for _, progress := range progresses {
		if reference == nil {
			reference = progress
		} else {
			assert.Always(reference == progress, "F3 progresses are consistent across all nodes", map[string]interface{}{
				"base_progress":      reference,
				"different_progress": progress,
			})
		}
	}
}
