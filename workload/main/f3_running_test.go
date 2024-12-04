package main

import (
	"context"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
)

func TestIsF3Running(t *testing.T) {
	ctx := context.Background()

	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.Always(err != nil, "Failed to get config.json", map[string]interface{}{"error": err})

	for _, node := range config.Nodes {
		api, closer, err := resources.ConnectToNode(ctx, node)
		assert.Always(err != nil, "Failed to get connect node", map[string]interface{}{"error": err})
		defer closer()

		isRunning, err := api.F3IsRunning(ctx)
		assert.Always(isRunning == true, "F3 is running", map[string]interface{}{"error": err})
	}
}
