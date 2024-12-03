package main

import (
	"context"
	"flag"
	"strings"
	"testing"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/stretchr/testify/assert"
)

var (
	nodes string
)

func init() {
	// Define and parse the custom flags before the `testing` package parses them
	flag.StringVar(&nodes, "nodes", "", "Comma-separated list of node names to test. Leave empty to test all nodes.")
}

func TestMain(m *testing.M) {
	flag.Parse() // Parse the custom flag along with the testing flags
	m.Run()      // Execute the tests
}

func TestNodeHeightProgression(t *testing.T) {
	ctx := context.Background()

	// Load the configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.NoError(t, err, "Failed to load config")

	// Parse node names from CLI argument
	nodeNames := parseNodeNames(nodes)
	if len(nodeNames) == 0 {
		t.Fatalf("No nodes specified for testing. Use the '-nodes' flag.")
	}

	// Iterate over the specified nodes in the CLI argument
	for _, nodeName := range nodeNames {
		t.Run(nodeName, func(t *testing.T) {
			nodeConfig, found := findNodeConfig(config, nodeName)
			if !found {
				t.Fatalf("Node '%s' not found in config.json", nodeName)
			}

			api, closer, err := resources.ConnectToNode(ctx, nodeConfig)
			assert.NoError(t, err, "Failed to connect to Lotus node")
			defer closer()

			// Get initial chain height
			initialHead, err := api.ChainHead(ctx)
			assert.NoError(t, err, "Failed to get initial chain head")
			initialHeight := int(initialHead.Height())
			// Wait for chain height progression
			timeout := 30 * time.Second // Adjust as needed
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()

			progressed := false
			for {
				select {
				case <-time.After(timeout):
					t.Fatalf("Node '%s' chain height did not progress within %s", nodeName, timeout)
				case <-ticker.C:
					currentHead, err := api.ChainHead(ctx)
					assert.NoError(t, err, "Failed to get current chain head")
					currentHeight := int(currentHead.Height())

					if currentHeight > initialHeight {
						t.Logf("Node '%s' chain height progressed: initial=%d, current=%d", nodeName, initialHeight, currentHeight)
						progressed = true
						break
					}
				}
				if progressed {
					break
				}
			}

			assert.True(t, progressed, "Node '%s' chain height did not progress", nodeName)
		})
	}
}

// Helper function to parse CLI-provided node names
func parseNodeNames(nodes string) []string {
	if nodes == "" {
		return nil
	}
	return strings.Split(nodes, ",")
}

// Helper function to find a node configuration by name
func findNodeConfig(config *resources.Config, nodeName string) (resources.NodeConfig, bool) {
	for _, node := range config.Nodes {
		if node.Name == nodeName {
			return node, true
		}
	}
	return resources.NodeConfig{}, false
}
