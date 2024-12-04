package main

import (
	"context"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/stretchr/testify/assert"
)

func TestTipsetConsistency(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.NoError(t, err, "Failed to load config")

	nodeNames := []string{"Lotus1", "Lotus2"}

	var filterNodes []resources.NodeConfig

	for _, node := range config.Nodes {
		for _, name := range nodeNames {
			if node.Name == name {
				filterNodes = append(filterNodes, node)
			}
		}
	}
	var tipsets []string
	for _, nodeName := range filterNodes {
		api, closer, err := resources.ConnectToNode(ctx, nodeName)
		defer closer()
		assert.NoError(t, err, "Failed")

		head, err := api.ChainHead(ctx)
		assert.NoError(t, err, "Failed")

		tipsets = append(tipsets, head.Key().String())
		assert.NoError(t, err, "Error")
	}

	// Verify all tipsets are identical
	for i := 1; i < len(tipsets); i++ {
		assert.Equal(t, tipsets[i], tipsets[0], "Tipsets are not consistent across nodes")
	}
}
