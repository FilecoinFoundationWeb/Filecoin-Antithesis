package main

import (
	"context"
	"io"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

const exchangeProtocol = "/fil/chain/xchg/0.0.1"

// openExchangeStream connects to the target and opens a ChainExchange stream.
func openExchangeStream(ctx context.Context, h host.Host, target peer.AddrInfo) (network.Stream, error) {
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := h.Connect(connectCtx, target); err != nil {
		return nil, err
	}

	streamCtx, streamCancel := context.WithTimeout(ctx, 10*time.Second)
	defer streamCancel()

	return h.NewStream(streamCtx, target.ID, exchangeProtocol)
}

// buildExchangeRequest builds a valid ChainExchange request as CBOR:
// Request = [Head []CID, Length uint64, Options uint64]
func buildExchangeRequest(head []cid.Cid, length uint64, options uint64) []byte {
	return cborArray(
		cborCIDArray(head),
		cborUint64(length),
		cborUint64(options),
	)
}

// readResponse reads up to 64KB from the stream, discarding the data.
func readResponse(s network.Stream) {
	s.SetReadDeadline(time.Now().Add(10 * time.Second))
	io.Copy(io.Discard, io.LimitReader(s, 64*1024))
}
