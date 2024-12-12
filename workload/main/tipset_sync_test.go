package main

import (
	"context"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

func TestTipsetConsistency(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.Always(err == nil, "Loading the resources config", map[string]interface{}{"error": err})

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
	for _, node := range filterNodes {
		api, closer, err := resources.ConnectToNode(ctx, node)
		assert.Always(err == nil, "Connecting to a node", map[string]interface{}{"node": node, "error": err})

		if err != nil {
			return
		}

		defer closer()

		ts, err := api.ChainHead(ctx)
		assert.Always(err == nil, "Getting the chainhead for a node", map[string]interface{}{"node": node, "error": err})

		if err != nil {
			return
		}

		tipsets = append(tipsets, ts.Key().String())
	}

	// Verify all tipsets are identical
	for i := 1; i < len(tipsets); i++ {
		assert.Always(tipsets[0] == tipsets[i], "Tipsets are not consistent across nodes", map[string]interface{}{"base_tipset": tipsets[0], "different_tipset": tipsets[i]})
	}
}
