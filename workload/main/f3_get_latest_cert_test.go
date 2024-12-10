package main

import (
	"context"
	"sync"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

func TestF3GetLatestCertificateEquality(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.Always(err == nil, "Failed to load config: %v", map[string]interface{}{"error": err})

	nodeNames := []string{"Lotus1", "Lotus2", "Forest"}
	var filterNodes []resources.NodeConfig

	// Filter nodes
	for _, node := range config.Nodes {
		for _, name := range nodeNames {
			if node.Name == name {
				filterNodes = append(filterNodes, node)
			}
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	latestCertificates := make(map[string]interface{})
	errors := make(map[string]interface{})

	// Fetch latest certificates concurrently
	for _, node := range filterNodes {
		wg.Add(1)
		go func(node resources.NodeConfig) {
			defer wg.Done()

			api, closer, err := resources.ConnectToNode(ctx, node)
			if err != nil {
				mu.Lock()
				errors[node.Name] = map[string]interface{}{"error": err, "message": "Failed to connect to node"}
				mu.Unlock()
				return
			}
			defer closer()
			latestCert, err := api.F3GetLatestCertificate(ctx)
			if err != nil {
				mu.Lock()
				errors[node.Name] = map[string]interface{}{"error": err, "message": "Failed to fetch latest certificate"}
				mu.Unlock()
				return
			}

			mu.Lock()
			latestCertificates[node.Name] = latestCert
			mu.Unlock()
		}(node)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Handle errors
	for node, err := range errors {
		assert.Always(false, "Node '%s' encountered an error: %v", map[string]interface{}{"node": node, "error": err})
	}

	// Assert all latest certificates are identical
	var reference interface{}
	for node, cert := range latestCertificates {
		if reference == nil {
			reference = cert
		} else {
			assert.Always(cert == reference, "Latest certificates are not consistent across nodes", map[string]interface{}{
				"node":     node,
				"expected": reference,
				"actual":   cert,
			})
		}
	}
}
