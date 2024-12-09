package main

import (
	"context"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	antithesis_assert "github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/stretchr/testify/assert"
)

func TestF3IsRunningEquality(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")

	antithesis_assert.Always(err == nil, "Load config", map[string]interface{}{"error": err})
	assert.NoError(t, err, "Failed to load config")

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

	for _, node := range filterNodes {
		api, closer, err := resources.ConnectToNode(ctx, node)

		antithesis_assert.Always(err == nil, "Connect to node", map[string]interface{}{"node": node.Name, "error": err})
		assert.NoError(t, err, "Failed to connect to node: %s", node.Name)

		defer closer()

		isRunning, err := api.F3IsRunning(ctx)
		antithesis_assert.Always(err == nil, "Fetch F3 running status from node", map[string]interface{}{"node": node.Name, "error": err})
		assert.NoError(t, err, "Failed to fetch F3 running status from node: %s", node.Name)

		if !isRunning {
			t.Logf("Node:%v not running F3", node.Name)
			antithesis_assert.Reachable("A node is not running F3", map[string]any{"node": node.Name})
		}

		if isRunning {
			t.Logf("Node:%v is running F3", node.Name)
			antithesis_assert.Reachable("A node is running F3", map[string]any{"node": node.Name})
		}
	}
}
