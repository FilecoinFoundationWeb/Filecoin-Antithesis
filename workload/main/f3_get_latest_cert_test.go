package main

import (
	"context"
	"testing"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
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

	var latestCertificates []interface{}
	for _, node := range filterNodes {
		api, closer, err := resources.ConnectToNode(ctx, node)
		assert.Always(err == nil, "Failed to connect to node: %s", map[string]interface{}{"node": node.Name})
		defer closer()

		latestCert, err := api.F3GetLatestCertificate(ctx)
		assert.Sometimes(err == nil, "Failed to fetch latest certificate from node: %s", map[string]interface{}{"node": node.Name, "error": err})
		latestCertificates = append(latestCertificates, latestCert)
	}

	// Assert all latest certificates are identical
	for i := 1; i < len(latestCertificates); i++ {
		assert.Always(latestCertificates[i] == latestCertificates[0], "Latest certificates are not consistent across nodes", map[string]interface{}{
			"expected": latestCertificates[0],
			"actual":   latestCertificates[i],
		})
	}
}
