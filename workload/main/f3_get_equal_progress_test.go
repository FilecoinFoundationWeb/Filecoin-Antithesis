package main

import (
	"context"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/stretchr/testify/assert"
)

func TestF3GetProgressEquality(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
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

	var progresses []interface{}
	for _, node := range filterNodes {
		api, closer, err := resources.ConnectToNode(ctx, node)
		assert.NoError(t, err, "Failed to connect to node: %s", node.Name)
		defer closer()

		progress, err := api.F3GetProgress(ctx)
		assert.NoError(t, err, "Failed to fetch F3 progress from node: %s", node.Name)
		progresses = append(progresses, progress)
	}

	// Assert all progresses are identical
	for i := 1; i < len(progresses); i++ {
		assert.Equal(t, progresses[i], progresses[0], "F3 progresses are not consistent across nodes")
	}
}
