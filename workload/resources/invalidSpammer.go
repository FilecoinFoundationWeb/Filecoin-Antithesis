package resources

import (
	context "context"
	cryptoRand "crypto/rand"
	"log"
	"math/big"
	mathRand "math/rand"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

var storedSignedMessages []*types.SignedMessage

func SendInvalidTransactions(ctx context.Context, api api.FullNode, from address.Address, to address.Address, count int) error {
	for i := 0; i < count; i++ {
		msg := &types.Message{
			To:         to,
			From:       from,
			Nonce:      0,
			Value:      abi.NewTokenAmount(0),
			GasLimit:   100000 + mathRand.Int63n(1000000),
			GasFeeCap:  abi.NewTokenAmount(1 + mathRand.Int63n(1e9)),
			GasPremium: abi.NewTokenAmount(1 + mathRand.Int63n(1e9)),
			Method:     abi.MethodNum(mathRand.Intn(100)),
			Params:     randomBytes(mathRand.Intn(64)),
		}

		// Apply mutations
		switch i % 8 {
		case 0:
			msg.GasPremium = abi.NewTokenAmount(100)
			msg.GasFeeCap = abi.NewTokenAmount(1) // GasPremium > GasFeeCap
		case 1:
			msg.GasLimit = -1000 // Invalid gas limit
		case 2:
			raw := new(big.Int).SetUint64(^uint64(0))
			msg.Value = abi.TokenAmount{Int: raw} // Very large value
		case 3:
			msg.Method = 99
			msg.Params = randomBytes(128) // Garbage params
		case 4:
			msg.To, _ = address.NewIDAddress(0) // Reserved ID
		case 5:
			msg.Params = randomBytes(2048) // Oversized
		case 6:
			msg.Params = []byte{0xff, 0x01, 0x02, 0x03} // Malformed CBOR
		case 7:
			msg.Params = []byte{0xa1, 0x63, 0x6a, 0x75, 0x6e, 0x6b, 0x58, 0x20} // CBOR junk
		}

		smsg, err := api.MpoolPushMessage(ctx, msg, nil)
		assert.Sometimes(err != nil, "expected to push message to mpool", nil)
		if err != nil {
			log.Printf("[rejected] tx %d: %v", i, err)
		} else {
			log.Printf("[ERROR] tx %d was accepted: %s", i, smsg.Cid())
			storedSignedMessages = append(storedSignedMessages, smsg)
		}
		time.Sleep(time.Millisecond * time.Duration(50+mathRand.Intn(200)))
	}
	return nil
}

func ReplayStoredSignedMessages(ctx context.Context, api api.FullNode) {
	for i, smsg := range storedSignedMessages {
		_, err := api.MpoolPush(ctx, smsg)
		if err != nil {
			log.Printf("[OK] Replay rejected (tx %d): %v", i, err)
		} else {
			log.Printf("[BUG] Replay accepted (tx %d): %s", i, smsg.Cid())
		}
	}
}

func randomBytes(n int) []byte {
	buf := make([]byte, n)
	_, _ = cryptoRand.Read(buf)
	return buf
}
