package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
)

func TestF3GetF3PowerTableEquality(t *testing.T) {
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

	var powerTables []string
	for _, node := range filterNodes {
		api, closer, err := resources.ConnectToNode(ctx, node)
		assert.Always(err == nil, "Failed to connect to node: %s", map[string]interface{}{"node": node.Name})
		defer closer()

		ts, err := api.ChainHead(ctx)
		assert.Always(err == nil, "Failed to get chain head for node: %s", map[string]interface{}{"node": node.Name, "error": err})

		powerTable, err := api.F3GetF3PowerTable(ctx, ts.Key())
		assert.Sometimes(err == nil, "Failed to fetch power table from node: %s", map[string]interface{}{"node": node.Name, "error": err})

		powerTableBytes, err := json.Marshal(powerTable)
		assert.Always(err == nil, "Failed to serialize power table from node: %s", map[string]interface{}{"node": node.Name, "error": err})
		powerTables = append(powerTables, string(powerTableBytes))
	}

	// Assert all power tables are identical
	for i := 1; i < len(powerTables); i++ {
		assert.Always(powerTables[i] == powerTables[0], "Power tables are not consistent across nodes", map[string]interface{}{
			"expected": powerTables[0],
			"actual":   powerTables[i],
		})
	}
}
