package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/stretchr/testify/assert"
)

func TestSamePowerTableAcrossNodes(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.NoError(t, err, "Failed to load config")

	// Nodes to test
	nodeNames := []string{"Lotus1", "Lotus2"}

	// Filter the nodes based on the specified node names
	var filteredNodes []resources.NodeConfig
	for _, node := range config.Nodes {
		for _, name := range nodeNames {
			if node.Name == name {
				filteredNodes = append(filteredNodes, node)
			}
		}
	}

	// Fetch power tables from the filtered nodes
	powerTables := make(map[string]string)
	for _, node := range filteredNodes {
		api, closer, err := resources.ConnectToNode(ctx, node)
		defer closer()
		assert.NoError(t, err, "Failed to connect to Lotus node")

		// Fetch the tipset key
		ts, err := api.ChainHead(ctx)
		fmt.Println(ts.Cids())
		assert.NoError(t, err, "Failed to get chain head")

		// Fetch power table
		powerTable, err := api.F3GetF3PowerTable(ctx, ts.Key())
		assert.NoError(t, err, "Failed to fetch power table")

		// Serialize power table to JSON for comparison
		powerTableBytes, err := json.Marshal(powerTable)
		assert.NoError(t, err, "Failed to serialize power table")
		powerTables[node.Name] = string(powerTableBytes)
		t.Logf("Node '%s' power table: %s", node.Name, powerTables[node.Name])
	}

	// Assert all power tables are the same
	var referenceTable string
	for name, table := range powerTables {
		if referenceTable == "" {
			referenceTable = table
		} else {
			assert.Equal(t, referenceTable, table, "Power tables do not match across nodes: Node '%s' has a different table", name)
		}
	}
}
