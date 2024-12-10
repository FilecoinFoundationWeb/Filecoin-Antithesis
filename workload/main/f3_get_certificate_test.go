package main

import (
	"context"
	"testing"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
)

func TestF3GetCertificateEquality(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.Always(err == nil, "Load config", map[string]interface{}{"error": err})

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

	var certificates []interface{}
	for _, node := range filterNodes {
		api, closer, err := resources.ConnectToNode(ctx, node)
		assert.Always(err == nil, "Connect to node", map[string]interface{}{"node": node.Name, "error": err})
		defer closer()

		certificate, err := api.F3GetCertificate(ctx, 1) // Example instance ID = 1
		assert.Sometimes(err == nil, "Fetch certificate from node", map[string]interface{}{"node": node.Name, "error": err})
		certificates = append(certificates, certificate)
	}

	// Assert all certificates are identical
	for i := 1; i < len(certificates); i++ {
		assert.Sometimes(certificates[i] == certificates[0], "Certificates are not consistent across nodes", map[string]interface{}{
			"expected": certificates[0],
			"actual":   certificates[i],
		})
	}
}
