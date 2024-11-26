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

	// Parse node names from CLI argument
	nodeNames := parseNodeNames(nodes)
	if len(nodeNames) == 0 {
		t.Fatalf("No nodes specified for testing. Use the '-nodes' flag.")
	}

	// Track tipsets for comparison
	var tipsets []string

	for _, nodeName := range nodeNames {
		t.Run(nodeName, func(t *testing.T) {
			nodeConfig, found := findNodeConfig(config, nodeName)
			if !found {
				t.Fatalf("Node '%s' not found in config.json", nodeName)
			}

			api, closer, err := resources.ConnectToNode(ctx, nodeConfig)
			assert.NoError(t, err, "Failed to connect to Lotus node")
			defer closer()

			head, err := api.ChainHead(ctx)
			assert.NoError(t, err, "Failed to get chain head")

			tipsets = append(tipsets, head.Key().String())
		})
	}

	// Verify all tipsets are identical
	for i := 1; i < len(tipsets); i++ {
		assert.Equal(t, tipsets[i], tipsets[0], "Tipsets are not consistent across nodes")
	}
}

