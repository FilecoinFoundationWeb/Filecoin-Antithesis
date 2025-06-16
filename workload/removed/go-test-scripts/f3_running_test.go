package main

import (
	"context"
	"sync"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
)

func TestF3IsRunningEquality(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

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
			head, err := api.ChainHead(ctx)
			if err != nil {
				t.Fatalf("Failed to get chain head: %v", err)
				return
			}
			height := head.Height()
			if height < 20 {
				t.Logf("Node: %v is not at height 20, skipping F3 running test", node.Name)
				return
			}
			if err != nil {
				t.Fatalf("Failed to get chain head: %v", err)
				return
			}
			defer closer()

			isRunning, err := api.F3IsRunning(ctx)

			if err != nil {
				t.Logf("Error fetching F3 status for node: %v", node.Name)
				return
			}

			if isRunning {
				t.Logf("Node: %v is running F3", node.Name)
			} else {
				t.Logf("Node: %v is not running F3", node.Name)
			}
		}(node)
	}

	// Wait for all goroutines to complete
	wg.Wait()
}
