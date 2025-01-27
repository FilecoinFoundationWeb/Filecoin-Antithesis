package main

import (
	"context"
	"os"
	"strings"
	"sync"
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

	// Filter nodes based on specified names
	var filterNodes []resources.NodeConfig
	for _, node := range config.Nodes {
		for _, name := range nodeNames {
			if node.Name == name {
				filterNodes = append(filterNodes, node)
			}
		}
	}

	var (
		mu      sync.Mutex
		tipsets = make([]string, len(filterNodes))
		epochs  = make([]int64, len(filterNodes)) // To store epoch numbers
		wg      sync.WaitGroup
	)

	// Fetch tipsets and epoch numbers concurrently
	for i, node := range filterNodes {
		wg.Add(1)
		go func(i int, node resources.NodeConfig) {
			defer wg.Done()
			api, closer, err := resources.ConnectToNode(ctx, node)
			assert.Always(err == nil, "Connecting to a node", map[string]interface{}{"node": node, "error": err})
			if err != nil {
				return
			}
			defer closer()

			// Fetch ChainHead
			head, err := api.ChainHead(ctx)
			assert.Always(err == nil, "Getting the chainhead for a node", map[string]interface{}{"node": node, "error": err})
			if err != nil {
				return
			}

			// Lock before writing shared data
			mu.Lock()
			tipsets[i] = head.Key().String()
			epochs[i] = int64(head.Height())
			mu.Unlock()

			t.Logf("Node '%s' tipset: %s, epoch: %d", node.Name, head.Key().String(), head.Height())
		}(i, node)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	if epochs[0] != epochs[1] {
		os.Exit(0)
	}

	ts0 := tipsets[0][1 : len(tipsets[0])-1]
	ts1 := tipsets[1][1 : len(tipsets[1])-1]

	ts0_split := strings.Split(ts0, ",")
	ts1_split := strings.Split(ts1, ",")

	one_equal := false

	for _, ts0_element := range ts0_split {
		for _, ts1_element := range ts1_split {
			if ts0_element == ts1_element {
				one_equal = true
			}
		}
	}

	assert.Always(one_equal, "Tipsets are consistent across nodes", map[string]interface{}{
		"base_tipset":        ts0_split,
		"other_tipset":       ts1_split,
		"epoch_number_base":  epochs[0],
		"epoch_number_other": epochs[1],
	})
}
