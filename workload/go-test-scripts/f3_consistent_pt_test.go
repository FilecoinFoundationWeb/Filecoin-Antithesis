package main

import (
	"context"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

func TestF3ApiCalls(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.Always(err == nil, "Workload: Loading the resources config", map[string]interface{}{"error": err})

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
		if node.Name == "Forest" {
			assert.Always(err == nil, "Forest: Successful http jsonrpc client connection", map[string]interface{}{"node": node.Name, "error": err})
		} else if node.Name == "Lotus1" || node.Name == "Lotus2" {
			assert.Always(err == nil, "Lotus: Successful http jsonrpc client connection", map[string]interface{}{"node": node.Name, "error": err})
		}
		if err != nil {
			continue
		}
		defer closer()

		// Test F3 API calls

		// F3GetManifest
		_, err = api.F3GetManifest(ctx)
		assert.Always(err == nil, "Lotus: Get the F3 Manifest", map[string]interface{}{"node": node.Name, "error": err})

		// F3GetECPowerTable
		ts, err := api.ChainHead(ctx)
		assert.Always(err == nil, "Workload: Getting the chainhead for a node", map[string]interface{}{"node": node.Name, "error": err})

		// F3GetF3PowerTable
		if err == nil {
			_, err = api.F3GetF3PowerTable(ctx, ts.Key())
			assert.Always(err == nil, "Lotus: Get the F3 PowerTable", map[string]interface{}{"node": node.Name, "error": err})
		}

		// F3IsRunning
		_, err = api.F3IsRunning(ctx)
		if node.Name == "Forest" {
			assert.Always(err == nil, "Forest: Fetching F3 running status", map[string]interface{}{"node": node.Name, "error": err})
		} else if node.Name == "Lotus1" || node.Name == "Lotus2" {
			assert.Always(err == nil, "Lotus: Fetching F3 running status", map[string]interface{}{"node": node.Name, "error": err})
		}

		// F3GetProgress
		_, err = api.F3GetProgress(ctx)
		assert.Always(err == nil, "Lotus: Get the F3 Progress", map[string]interface{}{"node": node.Name, "error": err})

		// F3ListParticipants
		_, err = api.F3ListParticipants(ctx)
		assert.Always(err == nil, "Lotus: List the F3 Participants", map[string]interface{}{"node": node.Name, "error": err})
	}
}
