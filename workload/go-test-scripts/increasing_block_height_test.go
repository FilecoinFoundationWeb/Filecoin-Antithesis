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

	var wg sync.WaitGroup

	for _, node := range filterNodes {
		wg.Add(1)
		go func(node resources.NodeConfig) {
			defer wg.Done()
			api, closer, err := resources.ConnectToNode(ctx, node)
			if err != nil {
				return
			}
			defer closer()

			for i := 0; i < 3; i++ {
				startTime := time.Now()

				initialts, err := api.ChainHead(ctx)

				if err != nil {
					return
				}

				for {
					api, closer, err := resources.ConnectToNode(ctx, node)

					if err != nil {
						return
					}
					defer closer()

					currentts, err := api.ChainHead(ctx)

					if err != nil {
						return
					}

					if currentts.Height() == initialts.Height()+1 || time.Since(startTime).Seconds() > 6 {
						duration := time.Since(startTime)
						assert.Always(duration.Seconds() <= 6,
							"[Block Time] Block height should increase within expected time window",
							resources.EnhanceAssertDetails(
								map[string]interface{}{
									"node":           node.Name,
									"initial_height": initialts.Height(),
									"current_height": currentts.Height(),
									"time_elapsed":   duration.Seconds(),
									"max_time":       6,
									"property":       "Block height progression timing",
									"impact":         "Critical - validates block production timing",
									"details":        "Block height must increase by 1 within 6 seconds",
									"iteration":      i,
								},
								node.Name,
							))
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
