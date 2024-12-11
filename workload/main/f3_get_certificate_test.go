package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

func TestF3GetCertificateEquality(t *testing.T) {
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

	var certificates []interface{}
	for _, node := range filterNodes {
		api, closer, err := resources.ConnectToNode(ctx, node)
		assert.Always(err == nil, "Connecting to a node", map[string]interface{}{"node": node.Name, "error": err})

		if err != nil {
			return
		}

		defer closer()

		certificate, err := api.F3GetCertificate(ctx, 1) // Example instance ID = 1
		assert.Sometimes(err == nil, "Fetching certificate from a node", map[string]interface{}{"node": node.Name, "error": err})

		// TO DELETE
		fmt.Print(certificate)

		if err != nil {
			return
		}

		certificates = append(certificates, certificate)
	}

	// Assert all certificates are identical
	for i := 1; i < len(certificates); i++ {

		assert.Always(certificates[0] == certificates[i], "All certificates are consistent across nodes", map[string]interface{}{
			"base_certificate":      certificates[0],
			"different_certificate": certificates[i],
		})
	}
}
