package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

func TestIncreasingBlockHeight(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.Always(err == nil, "Workload: Loading the resources config", map[string]interface{}{"error": err})

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

	var wg sync.WaitGroup

	for _, node := range filterNodes {
		wg.Add(1)
		go func(node resources.NodeConfig) {
			defer wg.Done()
			api, closer, err := resources.ConnectToNode(ctx, node)
			if node.Name == "Forest" {
				assert.Always(err == nil, "Forest: Successful http jsonrpc client connection", map[string]interface{}{"node": node.Name, "error": err})
			} else if node.Name == "Lotus1" || node.Name == "Lotus2" {
				assert.Always(err == nil, "Lotus: Successful http jsonrpc client connection", map[string]interface{}{"node": node.Name, "error": err})
			}
			if err != nil {
				return
			}
			defer closer()

			for i := 0; i < 3; i++ {
				startTime := time.Now()

				initialts, err := api.ChainHead(ctx)
				assert.Always(err == nil, "Workload: Getting the chainhead for a node", map[string]interface{}{"node": node.Name, "error": err})

				if err != nil {
					return
				}

				for {

					api, closer, err := resources.ConnectToNode(ctx, node)
					if node.Name == "Forest" {
						assert.Always(err == nil, "Forest: Successful http jsonrpc client connection", map[string]interface{}{"node": node.Name, "error": err})
					} else if node.Name == "Lotus1" || node.Name == "Lotus2" {
						assert.Always(err == nil, "Lotus: Successful http jsonrpc client connection", map[string]interface{}{"node": node.Name, "error": err})
					}

					if err != nil {
						return
					}
					defer closer()

					currentts, err := api.ChainHead(ctx)
					assert.Always(err == nil, "Workload: Getting the chainhead for a node", map[string]interface{}{"node": node.Name, "error": err})

					if err != nil {
						return
					}

					if currentts.Height() == initialts.Height()+1 || time.Since(startTime).Seconds() > 6 {
						duration := time.Since(startTime)
						assert.Always(duration.Seconds() <= 6, "Block height always increases by 1 within 6 seconds", map[string]interface{}{"node": node.Name, "initial_height": initialts.Height(), "current_height": currentts.Height(), "time_elapsed": duration.Seconds()})
					} else {
						time.Sleep(500 * time.Millisecond)
						continue
					}
					break
				}
			}
		}(node)
	}
	wg.Wait()
}
