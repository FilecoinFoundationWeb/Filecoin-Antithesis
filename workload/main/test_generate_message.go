package main

import (
	"context"
	"sync"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/antithesishq/antithesis-sdk-go/random"
	"github.com/test-go/testify/require"
	"go.dedis.ch/kyber/v4/sign/bdn"
)

func TestMessageBuilder(t *testing.T) {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.Always(err == nil, "Loading the resources config", map[string]interface{}{"error": err})

	// Ensure there are nodes in the configuration
	if len(config.Nodes) == 0 {
		t.Fatal("No nodes found in config.json")
	}

	nodeNames := []string{"Lotus1"}
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

			// Fetch the tipset key
			ts, err := api.ChainHead(ctx)
			assert.Always(err == nil, "Getting the chainhead for a node", map[string]interface{}{"node": node.Name, "error": err})
			if err != nil {
				return
			}

			// Fetch power table
			powerTable, err := api.F3GetF3PowerTable(ctx, ts.Key())
			assert.Always(err == nil, "Getting the F3 powertable for a node", map[string]interface{}{"node": node.Name, "error": err})
			if err != nil {
				return
			}

			//payload
			// main go f3 fix
			payload := resources.Payload{
				Instance: 1,
				Round:    0,
			}

			//get network name from lotus api api.get manifest and get manifest.networkname
			nn := resources.NetworkName("test")

			mt := &resources.MessageBuilder{
				NetworkName:      nn,
				PowerTable:       powerTable,
				Payload:          payload,
				SigningMarshaler: signingMarshaler,
			}

			// should be adding miner address, we can hard code it t01000 or something
			st, err = mt.PrepareSigningInputs(2)
			require.Error(t, err, "unknown ID should return an error")

			random.GetRandom()

			maliciousPrivKey := make([]byte, 256)

			for i := 0; i < 32; i++ {
				for j := 0; j < 8; j++ {
					maliciousPrivKey[i*8+j] = byte(random.GetRandom() >> (j * 8) & 0xFF)
				}
			}

			signer := resources.Signer{
				scheme:  bdn.NewSchemeOnG2(bls12381.NewSuiteBLS12381()),
				pubKey:  st.PubKey,
				privKey: maliciousPrivKey,
			}

			st.Sign(ctx, signer)

			return st

		}(node)
	}
	wg.Wait()
}
