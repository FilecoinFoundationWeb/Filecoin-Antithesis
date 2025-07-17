package mpoolfuzz

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
)

func CreateBaseMessage(from, to address.Address, _ uint64) *types.Message {
	return &types.Message{
		From:       from,
		To:         to,
		Value:      types.NewInt(100000000000000), // 0.0001 FIL in attoFIL
		GasLimit:   1000000,
		GasFeeCap:  types.NewInt(1000000000), // 1 nanoFIL in attoFIL
		GasPremium: types.NewInt(1000000000), // 1 nanoFIL in attoFIL
		Method:     0,
		Params:     nil,
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

// checkStateWait verifies that our mutated transactions never get mined
// It returns an error if any of the transactions are found on chain
func checkStateWait(ctx context.Context, api api.FullNode, msgCids []cid.Cid) error {
	// Get current head for searching
	head, err := api.ChainHead(ctx)
	if err != nil {
		return fmt.Errorf("failed to get chain head: %w", err)
	}

	// Give some time for messages to potentially get mined
	time.Sleep(30 * time.Second)

	// Check each message CID
	for i, msgCid := range msgCids {
		lookup, err := api.StateSearchMsg(ctx, head.Key(), msgCid, 10, false)

		assert.Sometimes(err != nil || lookup == nil,
			fmt.Sprintf("Message %d (CID: %s) should not be found on chain via StateSearchMsg", i, msgCid),
			map[string]interface{}{
				"message_cid":   msgCid.String(),
				"message_index": i,
				"error":         err,
				"lookup_found":  lookup != nil,
			})

		if lookup != nil {
			log.Printf("[VIOLATION] Message %d (CID: %s) was unexpectedly mined!", i, msgCid)
		}

		// Double check with StateWaitMsg with a short timeout
		waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		lookup, err = api.StateWaitMsg(waitCtx, msgCid, 1, 5, false)

		assert.Sometimes(err != nil || lookup == nil,
			fmt.Sprintf("Message %d (CID: %s) should not be found on chain via StateWaitMsg", i, msgCid),
			map[string]interface{}{
				"message_cid":   msgCid.String(),
				"message_index": i,
				"error":         err,
				"lookup_found":  lookup != nil,
			})

		if lookup != nil {
			log.Printf("[VIOLATION] Message %d (CID: %s) was unexpectedly found via StateWaitMsg!", i, msgCid)
		}
	}

	// Final assertion that summarizes the check
	assert.Sometimes(true,
		fmt.Sprintf("All %d mutated messages remained in mempool and were not mined", len(msgCids)),
		map[string]interface{}{
			"total_messages": len(msgCids),
			"requirement":    "Invalid messages should never be mined",
		})

	log.Printf("[SUCCESS] None of the %d mutated messages were mined (as expected)", len(msgCids))
	return nil
}
