package main

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

func TestSamePowerTableAcrossNodes(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.Always(err == nil, "Workload: Loading the resources config", map[string]interface{}{"error": err})

	// Ensure there are nodes in the configuration
	if len(config.Nodes) == 0 {
		t.Fatal("No nodes found in config.json")
	}

	var wg sync.WaitGroup
	powerTables := make([]string, len(config.Nodes))
	errChan := make(chan error, len(config.Nodes))

	for i, node := range config.Nodes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			api, closer, err := resources.ConnectToNode(ctx, node)
			if node.Name == "Forest" {
				assert.Always(err == nil, "Forest: Successful http jsonrpc client connection", map[string]interface{}{"node": node.Name, "error": err})
			} else if node.Name == "Lotus1" || node.Name == "Lotus2" {
				assert.Always(err == nil, "Lotus: Successful http jsonrpc client connection", map[string]interface{}{"node": node.Name, "error": err})
			}
			if err != nil {
				errChan <- err
				return
			}
			defer closer()

			// Fetch the tipset key
			ts, err := api.ChainHead(ctx)
			assert.Always(err == nil, "Getting the chainhead for a node", map[string]interface{}{"node": node.Name, "error": err})
			if err != nil {
				errChan <- err
				return
			}

			// Fetch power table
			powerTable, err := api.F3GetF3PowerTable(ctx, ts.Key())
			assert.Always(err == nil, "Getting the F3 powertable for a node", map[string]interface{}{"node": node.Name, "error": err})
			if err != nil {
				errChan <- err
				return
			}

			// Serialize power table to JSON for comparison
			powerTableBytes, err := json.Marshal(powerTable)
			assert.Always(err == nil, "Serialized the powertable", map[string]interface{}{"node": node.Name, "error": err})
			if err != nil {
				errChan <- err
				return
			}
			powerTables[i] = string(powerTableBytes)
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		assert.Unreachable("An error occurred during a PowerTable fetch", map[string]interface{}{"error": err})
	}

	// Assert all power tables are the same
	for i := 1; i < len(powerTables); i++ {
		assert.Always(powerTables[0] == powerTables[i], "All power tables match across nodes", map[string]interface{}{
			"base_powertable":     powerTables[0],
			"compared_powertable": powerTables[i],
		})
	}
}
