package main

import (
	"context"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

func TestBackfill(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.Always(err == nil, "Loading the resources config", map[string]interface{}{"error": err})

	// Hardcoded list of Lotus nodes to test
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

	for _, node := range filteredNodes {
		api, closer, err := resources.ConnectToNode(ctx, node)
		assert.Always(err == nil, "Connecting to a node", map[string]interface{}{"node": node.Name, "error": err})
		if err != nil {
			continue
		}
		defer closer()

		// Test backfill
		head, _ := api.ChainHead(ctx)
		_, err = api.ChainValidateIndex(ctx, head.Height()-1, true)
		assert.Always(err == nil, "ChainValidateIndex call successful", map[string]interface{}{"node": node.Name, "error": err})

	}
}
