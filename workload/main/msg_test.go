package main

import (
	"context"
	"log"
	"testing"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-f3/gpbft"
	"github.com/filecoin-project/lotus/api"
	"golang.org/x/xerrors"
)

type testSigningMarshaler struct{}

func (testSigningMarshaler) MarshalPayloadForSigning(nn gpbft.NetworkName, p *gpbft.Payload) []byte {
	return p.MarshalForSigning(nn)
}

type LotusAPISigner struct {
	API api.FullNode
}

func (s *LotusAPISigner) Sign(ctx context.Context, sender gpbft.PubKey, msg []byte) ([]byte, error) {
	if s.API == nil {
		return nil, xerrors.Errorf("API is not initialized")
	}

	addr, err := address.NewBLSAddress(sender[:])
	if err != nil {
		return nil, xerrors.Errorf("converting pubkey to address: %w", err)
	}

	sig, err := s.API.WalletSign(ctx, addr, msg)
	if err != nil {
		return nil, xerrors.Errorf("error while signing: %w", err)
	}

	return sig.Data, nil
}

func TestBuildGMessageWithSignatureBuilder(t *testing.T) {
	ctx := context.Background()

	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	assert.Always(err == nil, "Loading the resources config", map[string]interface{}{"error": err})

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
		assert.Always(err == nil, "Connecting to a node", map[string]interface{}{"node": node, "error": err})

		if err != nil {
			continue
		}
		defer closer()

		tipset, err := api.ChainHead(ctx)
		assert.Always(err == nil, "Fetching chain head", map[string]interface{}{"node": node, "error": err})

		powerEntries, err := api.F3GetF3PowerTable(ctx, tipset.Key())
		assert.Always(err == nil, "Fetching power table", map[string]interface{}{"node": node, "error": err})

		pt := gpbft.NewPowerTable()
		err = pt.Add(powerEntries...)
		assert.Always(err == nil, "Adding power entries to power table", map[string]interface{}{"node": node, "error": err})

		manifest, err := api.F3GetManifest(ctx)
		assert.Always(err == nil, "Fetching manifest", map[string]interface{}{"node": node, "error": err})

		payload := gpbft.Payload{
			Instance: 1,
			Round:    0,
			Phase:    2,
		}

		mb := &gpbft.MessageBuilder{
			NetworkName:      gpbft.NetworkName(manifest.NetworkName),
			PowerTable:       pt,
			Payload:          payload,
			SigningMarshaler: testSigningMarshaler{},
		}

		signer := &LotusAPISigner{API: api}
		minerID := uint64(1000)

		signatureBuilder, err := mb.PrepareSigningInputs(gpbft.ActorID(minerID))
		assert.Always(err == nil, "Preparing signing inputs", map[string]interface{}{"node": node, "minerID": minerID, "error": err})

		if signatureBuilder.PayloadToSign == nil {
			assert.Always(false, "PayloadToSign should not be nil", map[string]interface{}{"node": node, "minerID": minerID})
			continue
		}

		payloadSig, _, err := signatureBuilder.Sign(ctx, signer)
		assert.Always(err == nil, "Signing payload", map[string]interface{}{"node": node, "minerID": minerID, "error": err})

		gMessage := signatureBuilder.Build(payloadSig, nil)
		log.Printf("Constructed GMessage for Node '%s': %+v\n", node.Name, gMessage)
	}
}
