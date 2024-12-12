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
	assert.Always(err == nil, "Loading the resources config", map[string]interface{}{"error": err})

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
	powerTables := make(map[string]string)

	// Fetch power tables concurrently
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

			ts, err := api.ChainHead(ctx)
			assert.Always(err == nil, "Getting the chainhead for a node", map[string]interface{}{"node": node.Name, "error": err})

			if err != nil {
				return
			}

			powerTable, err := api.F3GetF3PowerTable(ctx, ts.Key())
			assert.Always(err == nil, "Getting the F3 powertable for a node", map[string]interface{}{"node": node.Name, "error": err})

			if err != nil {
				return
			}

			powerTableBytes, err := json.Marshal(powerTable)
			assert.Always(err == nil, "Serialized the powertable", map[string]interface{}{"node": node.Name, "error": err})

			if err != nil {
				return
			}

			powerTables[node.Name] = string(powerTableBytes)
		}(node)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Not enough powertables to compare with one another
	if len(powerTables) < 2 {
		return
	}

	// Assert all power tables are identical
	var reference string
	for _, table := range powerTables {
		if reference == "" {
			reference = table
		} else {
			assert.Always(table == reference, "All power tables are consistent across nodes", map[string]interface{}{
				"base_powertable":      reference,
				"different_powertable": table,
			})
		}
	}
}
