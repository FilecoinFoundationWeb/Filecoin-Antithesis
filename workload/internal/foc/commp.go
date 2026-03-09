package foc

import (
	"encoding/binary"
	"fmt"
	"math/bits"

	commwriter "github.com/filecoin-project/go-commp-utils/v2/writer"
	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
)

const (
	// PieceCIDv2 multihash code: fr32-sha2-256-trunc254-padded-binary-tree
	mhPieceCIDv2 = 0x1011
	// PieceCIDv2 CID codec: raw
	codecRaw = 0x55
)

// CalculatePieceCID computes the Filecoin piece commitment (CommP) for the given data.
// Returns a PieceCIDv2 string as required by Curio's PDP API.
// Digest format per FRC-0069: [padding (varint)][height (1 byte)][root (32 bytes)]
func CalculatePieceCID(data []byte) (string, error) {
	w := &commwriter.Writer{}
	if _, err := w.Write(data); err != nil {
		return "", fmt.Errorf("commp write: %w", err)
	}
	result, err := w.Sum()
	if err != nil {
		return "", fmt.Errorf("commp sum: %w", err)
	}

	// Extract the 32-byte CommP root from the PieceCIDv1
	decoded, err := mh.Decode(result.PieceCID.Hash())
	if err != nil {
		return "", fmt.Errorf("decode multihash: %w", err)
	}

	// height = log2(paddedPieceSize / 32) = log2(paddedPieceSize) - 5
	height := byte(bits.TrailingZeros64(uint64(result.PieceSize)) - 5)

	// padding = unpaddedPieceSize - payloadSize
	unpadded := uint64(result.PieceSize) / 128 * 127
	padding := unpadded - uint64(len(data))

	// Build digest: [padding (varint)] [height (1 byte)] [root (32 bytes)]
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], padding)
	digest := make([]byte, n+1+len(decoded.Digest))
	copy(digest, buf[:n])
	digest[n] = height
	copy(digest[n+1:], decoded.Digest)

	// Encode as multihash with PieceCIDv2 hash code
	encoded, err := mh.Encode(digest, mhPieceCIDv2)
	if err != nil {
		return "", fmt.Errorf("encode multihash: %w", err)
	}

	return cid.NewCidV1(codecRaw, encoded).String(), nil
}
