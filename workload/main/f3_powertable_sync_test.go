package main

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

func TestF3GetF3PowerTableEquality(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.Always(err == nil, "Load config", map[string]interface{}{"error": err})

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
	powerTables := make(map[string]string)
	errors := make(map[string]interface{})

	// Fetch power tables concurrently
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

			ts, err := api.ChainHead(ctx)
			if err != nil {
				mu.Lock()
				errors[node.Name] = map[string]interface{}{"error": err, "message": "Failed to get chain head"}
				mu.Unlock()
				return
			}

			powerTable, err := api.F3GetF3PowerTable(ctx, ts.Key())

			if err != nil {
				mu.Lock()
				errors[node.Name] = map[string]interface{}{"error": err, "message": "Failed to fetch power table"}
				mu.Unlock()
				return
			}

			powerTableBytes, err := json.Marshal(powerTable)
			if err != nil {
				mu.Lock()
				errors[node.Name] = map[string]interface{}{"error": err, "message": "Failed to serialize power table"}
				mu.Unlock()
				return
			}

			mu.Lock()
			powerTables[node.Name] = string(powerTableBytes)
			mu.Unlock()
		}(node)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Handle errors
	for node, err := range errors {
		assert.Always(false, "Node '%s' encountered an error: %v", map[string]interface{}{"node": node, "error": err})
	}

	// Assert all power tables are identical
	var reference string
	for node, table := range powerTables {
		if reference == "" {
			reference = table
		} else {
			assert.Always(table == reference, "Power tables are not consistent across nodes", map[string]interface{}{
				"node":     node,
				"expected": reference,
				"actual":   table,
			})
		}
	}
}
