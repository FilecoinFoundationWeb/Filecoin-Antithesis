package mpoolfuzz

import (
	"context"
	"crypto/rand"
	"log"
	"math/big"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
)

func CreateBaseMessage(from, to address.Address, nonce uint64) *types.Message {
	return &types.Message{
		From:       from,
		To:         to,
		Value:      types.NewInt(100000000000000), // 0.0001 FIL in attoFIL
		GasLimit:   1000000,
		GasFeeCap:  types.NewInt(1000000000), // 1 nanoFIL in attoFIL
		GasPremium: types.NewInt(1000000000), // 1 nanoFIL in attoFIL
		Method:     0,
		Params:     nil,
		// Nonce is omitted - MpoolPushMessage will set it automatically
	}
}

func RandomBytes(n int) []byte {
	buff := make([]byte, n)
	rand.Read(buff)
	return buff
}

func GenerateRandomAddress() (address.Address, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return address.Undef, err
	}
	return address.NewIDAddress(n.Uint64())
}

func IsMessageOnChain(ctx context.Context, api api.FullNode, msgCid cid.Cid) (bool, error) {
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	lookup, _ := api.StateWaitMsg(waitCtx, msgCid, 1, abi.ChainEpoch(-1), false)
	cancel()
	return lookup != nil, nil
}

// checkStateWait verifies that our mutated transactions never get mined
// It returns an error if any of the transactions are found on chain
func checkStateWait(ctx context.Context, api api.FullNode, msgCids []cid.Cid, mutationDescriptions []string) error {
	// Give some time for messages to potentially get mined
	time.Sleep(60 * time.Second)

	foundOnChain := false
	for i, msgCid := range msgCids {
		description := "unknown mutation"
		if i < len(mutationDescriptions) {
			description = mutationDescriptions[i]
		}

		// Check with StateWaitMsg with a short timeout
		waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		lookup, _ := api.StateWaitMsg(waitCtx, msgCid, 1, abi.ChainEpoch(-1), false)
		cancel()

		if lookup != nil {
			log.Printf("[VIOLATION] Message %d (CID: %s) [%s] was unexpectedly found via StateWaitMsg!", i, msgCid, description)
			foundOnChain = true
		}
	}

	assert.Sometimes(!foundOnChain, "No mutated messages should be found on chain", map[string]interface{}{
		"total_messages": len(msgCids),
		"requirement":    "Invalid messages should never be mined",
	})

	log.Printf("[SUCCESS] Checked %d mutated messages", len(msgCids))
	return nil
}
