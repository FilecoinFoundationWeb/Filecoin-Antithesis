package main

import (
	"context"
	"fmt"
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

	for _, node := range config.Nodes {
		wg.Add(1)
		go func(node resources.NodeConfig) {
			defer wg.Done()
			api, closer, err := resources.ConnectToNode(ctx, node)
			assert.Always(err == nil, "Connecting to a node", map[string]interface{}{"node": node.Name, "error": err})
			if err != nil {
				return
			}
			defer closer()

			for i := 0; i < 3; i++ {
				startTime := time.Now()

				initialts, err := api.ChainHead(ctx)
				assert.Always(err == nil, "Getting the chainhead for a node", map[string]interface{}{"node": node.Name, "error": err})

				if err != nil {
					return
				}

				for {
					currentts, err := api.ChainHead(ctx)
					assert.Always(err == nil, "Getting the chainhead for a node", map[string]interface{}{"node": node.Name, "error": err})

					if err != nil {
						return
					}

					if currentts.Height() == initialts.Height()+1 {
						duration := time.Since(startTime)
						if duration.Seconds() <= 7.5 {
							fmt.Print(initialts)
							fmt.Print(currentts)
							fmt.Print(duration.Seconds())
						} else {
							fmt.Print(initialts)
							fmt.Print(currentts)
							fmt.Print(duration.Seconds())
						}
					} else {
						fmt.Print("retry in .5 seconds")
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
