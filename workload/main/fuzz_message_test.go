//go:build fuzzing

package main

import (
	"context"
	"log"
	"testing"
	"time"

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
func (fff *F3) FuzzBuildAndSignMessages(f *testing.F) {
	const timeoutDuration = 10 * time.Second

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

	assert.Always(len(filteredNodes) > 0, "No nodes found for fuzzing", map[string]interface{}{"nodes": nodeNames})

	f.Add(uint64(1), uint64(0), uint8(2))

	f.Fuzz(func(t *testing.T, instance, round uint64, phase uint8) {
		for _, node := range filteredNodes {
			ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)
			defer cancel()

			api, closer, err := resources.ConnectToNode(ctx, node)
			assert.Always(err == nil, "Connecting to a Lotus node", map[string]interface{}{"node": node.Name, "error": err})
			if err != nil {
				continue
			}
			defer closer()

			tipset, err := api.ChainHead(ctx)
			assert.Always(err == nil, "Fetching chain head", map[string]interface{}{"node": node.Name, "error": err})
			if err != nil {
				continue
			}

			powerEntries, err := api.F3GetF3PowerTable(ctx, tipset.Key())
			assert.Always(err == nil, "Fetching power table", map[string]interface{}{"node": node.Name, "error": err})
			if err != nil {
				continue
			}

			pt := gpbft.NewPowerTable()
			err = pt.Add(powerEntries...)
			assert.Always(err == nil, "Adding power entries to power table", map[string]interface{}{"node": node.Name, "error": err})
			if err != nil {
				continue
			}

			manifest, err := api.F3GetManifest(ctx)
			assert.Always(err == nil, "Fetching manifest", map[string]interface{}{"node": node.Name, "error": err})
			if err != nil {
				continue
			}

			payload := gpbft.Payload{
				Instance: instance,
				Round:    round,
				Phase:    gpbft.Phase(phase),
			}

			serialized := payload.MarshalForSigning(gpbft.NetworkName(manifest.NetworkName))
			assert.Always(len(serialized) > 0, "Serialized payload is empty", map[string]interface{}{"payload": payload})

			malformed := make([]byte, len(serialized))
			copy(malformed, serialized)
			for i := range malformed {
				if i%5 == 0 {
					malformed[i] ^= 0xFF
				}
			}
			malformed = append(malformed, []byte("!!!INVALID###")...)
			log.Printf("Fuzzing with malformed payload: %x", malformed)

			mb := &gpbft.MessageBuilder{
				NetworkName:      gpbft.NetworkName(manifest.NetworkName),
				PowerTable:       pt,
				Payload:          payload,
				SigningMarshaler: testSigningMarshaler{},
			}

			signer := &LotusAPISigner{API: api}
			minerID := uint64(1000)

			signatureBuilder, err := mb.PrepareSigningInputs(gpbft.ActorID(minerID))
			assert.Sometimes(err == nil, "Preparing signing inputs", map[string]interface{}{"node": node.Name, "error": err})
			if err != nil {
				continue
			}

			payloadSig, _, err := signatureBuilder.Sign(ctx, signer)
			assert.Sometimes(err == nil, "Signing the payload", map[string]interface{}{"node": node.Name, "error": err})
			if err != nil {
				continue
			}

			gMessage := signatureBuilder.Build(payloadSig, nil)
			log.Printf("Constructed GMessage with malformed payload: %+v", gMessage)
			assert.Always(gMessage == nil, "Constructed GMessage is nil", map[string]interface{}{"node": node.Name})
		}
	})
}
