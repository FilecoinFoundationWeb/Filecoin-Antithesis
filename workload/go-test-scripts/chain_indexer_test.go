package main

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-state-types/abi"
)

func TestChainIndexer(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.Always(err == nil, "Loading the resources config", map[string]interface{}{"error": err})

	nodeNames := []string{"Lotus1", "Lotus2"}
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
		assert.Always(err == nil, "Connecting to a node", map[string]interface{}{"node": node, "error": err})
		if err != nil {
			return
		}
		defer closer()

		// Get the latest tipset
		ts, err := api.ChainHead(ctx)
		assert.Always(err == nil, "Getting latest tipset", map[string]interface{}{"node": node, "error": err})

		// Get the latest height
		latestHeight := ts.Height()

		// Select a random height before the latest one
		rand.Seed(time.Now().UnixNano())
		randomHeight := abi.ChainEpoch(rand.Int63n(int64(latestHeight)))

		// Validate the chain index at the selected height
		valid, err := api.ChainValidateIndex(ctx, randomHeight, false)
		assert.Always(err == nil, "Validating chain index", map[string]interface{}{
			"node":          node,
			"randomHeight":  randomHeight,
			"latestHeight":  latestHeight,
			"validationRes": valid,
			"error":         err,
		})
		fmt.Println("Chain index validation result: ", valid)
	}
}
