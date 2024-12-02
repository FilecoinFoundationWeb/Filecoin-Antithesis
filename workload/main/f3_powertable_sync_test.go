package main

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/stretchr/testify/assert"
)

func TestSamePowerTableAcrossNodes(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.NoError(t, err, "Failed to load config")

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
			if err != nil {
				errChan <- err
				return
			}
			defer closer()

			// Fetch the tipset key
			ts, err := api.ChainHead(ctx)
			if err != nil {
				errChan <- err
				return
			}

			// Fetch power table
			powerTable, err := api.F3GetF3PowerTable(ctx, ts.Key())
			if err != nil {
				errChan <- err
				return
			}

			// Serialize power table to JSON for comparison
			powerTableBytes, err := json.Marshal(powerTable)
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
		assert.NoError(t, err, "Error occurred during power table fetch")
	}

	// Assert all power tables are the same
	for i := 1; i < len(powerTables); i++ {
		assert.Equal(t, powerTables[0], powerTables[i], "Power tables do not match across nodes")
	}
}
