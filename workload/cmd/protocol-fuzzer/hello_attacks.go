package main

import (
	"context"
	"log"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multihash"
)

const helloProtocol = "/fil/hello/1.0.0"

// buildHelloMessage builds a Hello protocol message as CBOR.
// HelloMessage wire format: [HeaviestTipSet []CID, HeaviestTipSetHeight int64, HeaviestTipSetWeight BigInt-bytes, GenesisHash CID]
func buildHelloMessage(tipset []cid.Cid, height uint64, weight uint64, genesis cid.Cid) []byte {
	return cborArray(
		cborCIDArray(tipset),
		cborUint64(height),
		cborBytes(bigIntBytes(weight)),
		cborCID(genesis),
	)
}

// parseGenesisCID converts the genesis CID string discovered at startup to a cid.Cid.
func parseGenesisCID() cid.Cid {
	if genesisCID == "" {
		return randomCID()
	}
	c, err := cid.Decode(genesisCID)
	if err != nil {
		log.Printf("[protocol-fuzzer] cannot parse genesis CID %q: %v, using random", genesisCID, err)
		return randomCID()
	}
	return c
}

// openHelloStream connects to the target and opens a Hello protocol stream.
func openHelloStream(ctx context.Context, h host.Host, target peer.AddrInfo) (*wrappedStream, error) {
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := h.Connect(connectCtx, target); err != nil {
		return nil, err
	}

	streamCtx, streamCancel := context.WithTimeout(ctx, 10*time.Second)
	defer streamCancel()

	s, err := h.NewStream(streamCtx, target.ID, helloProtocol)
	if err != nil {
		return nil, err
	}
	return &wrappedStream{s}, nil
}

// wrappedStream wraps network.Stream to provide convenience methods.
type wrappedStream struct {
	s interface {
		Write([]byte) (int, error)
		Read([]byte) (int, error)
		Close() error
		CloseWrite() error
		Reset() error
	}
}

func (w *wrappedStream) Write(b []byte) (int, error) { return w.s.Write(b) }
func (w *wrappedStream) Close() error                { return w.s.Close() }
func (w *wrappedStream) CloseWrite() error           { return w.s.CloseWrite() }
func (w *wrappedStream) Reset() error                { return w.s.Reset() }

// Ensure the multihash import is used (needed for randomCID in cbor_helpers.go).
var _ = multihash.SHA2_256
