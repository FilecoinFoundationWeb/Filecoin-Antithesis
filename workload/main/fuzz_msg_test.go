package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/test-go/testify/assert"
)

// import (
// 	"context"
// 	"fmt"
// 	"testing"

// 	"github.com/filecoin-project/go-address"
// 	"github.com/filecoin-project/go-f3/gpbft"
// 	"github.com/filecoin-project/go-f3/msg_encoding"
// 	"github.com/filecoin-project/go-state-types/crypto"
// 	"github.com/filecoin-project/lotus/api"
// 	"github.com/stretchr/testify/assert"

// 	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources" // Replace with the path to your resources package
// )

// // LotusSigner wraps the Lotus API for signing messages
// type LotusSigner struct {
// 	API     api.FullNode
// 	Address address.Address
// }

// // Sign uses the Lotus API to sign data
// func (ls *LotusSigner) Sign(ctx context.Context, data []byte) (*crypto.Signature, error) {
// 	return ls.API.WalletSign(ctx, ls.Address, data)
// }

// // FuzzInvalidMessages tests invalid/random messages for edge cases across multiple Lotus nodes
// func FuzzInvalidMessages(f *testing.F) {
// 	ctx := context.Background()

// 	// Step 1: Load configuration
// 	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
// 	assert.NoError(f, err, "Failed to load config")

// 	// Hardcoded list of Lotus nodes to test
// 	nodeNames := []string{"Lotus1", "Lotus2"}

// 	// Step 2: Filter nodes based on nodeNames
// 	var filteredNodes []resources.NodeConfig
// 	for _, node := range config.Nodes {
// 		for _, name := range nodeNames {
// 			if node.Name == name {
// 				filteredNodes = append(filteredNodes, node)
// 			}
// 		}
// 	}

// 	assert.Greater(f, len(filteredNodes), 0, "No nodes matched the specified names")

// 	// Seed initial data for fuzzing
// 	f.Add(uint64(12345), []byte("valid_payload"))

// 	// Step 3: Connect to filtered nodes
// 	for _, node := range filteredNodes {
// 		node := node // Capture range variable
// 		f.Fuzz(func(t *testing.T, nonce uint64, payload []byte) {
// 			api, closer, err := resources.ConnectToNode(ctx, node)
// 			assert.NoError(t, err, "Failed to connect to Lotus node")
// 			defer closer()

// 			// Step 4: Fetch signer address
// 			signerAddr, err := api.WalletDefaultAddress(ctx)
// 			assert.NoError(t, err, "Failed to fetch default wallet address")

// 			// Step 5: Build a fuzzed message
// 			randomAddr, _ := address.NewIDAddress(nonce)
// 			randomPayload := msg_encoding.RandomBytes(32)

// 			msg := gpbft.MessageBuilder{
// 				Address: randomAddr.String(),
// 				Payload: randomPayload,
// 				Nonce:   nonce,
// 			}

// 			// Step 6: Sign the fuzzed message
// 			signer := &LotusSigner{API: api, Address: signerAddr}
// 			messageBuilder := gpbft.NewMessageBuilder{}
// 			signedMsg, err := messageBuilder.Sign(ctx, signer, msg)

// 			if err != nil {
// 				t.Logf("Message signing failed (expected for invalid input): %v", err)
// 				return
// 			}
// 			fmt.Println(signedMsg)
// 		})
// 	}
// }

func TestMessageCreate(t *testing.T) {
	ctx := context.Background()
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.NoError(t, err, "Failed to load config")

	nodeNames := []string{"Lotus1"}

	var filteredNodes []resources.NodeConfig
	for _, node := range config.Nodes {
		for _, name := range nodeNames {
			if node.Name == name {
				filteredNodes = append(filteredNodes, node)
			}
		}
	}
	for _, node := range filteredNodes {
		api, closer, err := resources.ConnectToNode(ctx, node)
		assert.NoError(t, err)

		if err != nil {
			return
		}
		defer closer()
		tipset, err := api.ChainHead(ctx)
		assert.NoError(t, err)

		pt, err := api.F3GetF3PowerTable(ctx, tipset.Key())
		assert.NoError(t, err)

		manifest, err := api.F3GetManifest(ctx)
		assert.NoError(t, err)

		fmt.Println(manifest)
		fmt.Println(pt)
	}

}
