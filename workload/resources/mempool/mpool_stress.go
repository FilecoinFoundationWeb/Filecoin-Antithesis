package mempool

import (
	"context"
	"crypto/rand"
	"log"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

const MaxMessageSize = 64 << 10

const MaxMessagesPerBlock = 5000

// SendMaxSizedMessage creates and sends a message with a large amount of data.
func SendMaxSizedMessage(ctx context.Context, nodeAPI api.FullNode) error {
	log.Println("[INFO] Starting test for maximum message size...")

	fromAddr, err := nodeAPI.WalletDefaultAddress(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to get default wallet: %v", err)
		return nil
	}

	toAddr, err := nodeAPI.WalletNew(ctx, types.KTSecp256k1)
	if err != nil {
		log.Printf("[ERROR] Failed to create new wallet for destination: %v", err)
		return nil
	}
	log.Printf("[INFO] Sending max sized message from %s to %s", fromAddr, toAddr)

	params := make([]byte, MaxMessageSize)
	if _, err := rand.Read(params); err != nil {
		log.Printf("[ERROR] Failed to generate random params: %v", err)
		return nil
	}

	msg := &types.Message{
		To:     toAddr,
		From:   fromAddr,
		Value:  abi.NewTokenAmount(0),
		Method: 0,
		Params: params,
	}

	gasMsg, err := nodeAPI.GasEstimateMessageGas(ctx, msg, nil, types.EmptyTSK)
	if err != nil {
		log.Printf("[ERROR] Failed to estimate gas: %v", err)
		return nil
	}
	msg.GasLimit = gasMsg.GasLimit
	msg.GasFeeCap = gasMsg.GasFeeCap
	msg.GasPremium = gasMsg.GasPremium

	log.Printf("[INFO] Pushing message with size %d bytes to mempool", len(params))
	signedMsg, err := nodeAPI.MpoolPushMessage(ctx, msg, nil)
	if err != nil {
		log.Printf("[ERROR] Failed to push max-sized message: %v", err)
		return nil
	}

	log.Printf("[INFO] Max-sized message sent successfully, CID: %s", signedMsg.Cid())
	return nil
}

func SendMaxMessages(ctx context.Context, nodeAPI api.FullNode) error {
	log.Println("[INFO] Starting test for sending 200 messages...")

	fromAddr, err := nodeAPI.WalletDefaultAddress(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to get default wallet: %v", err)
		return nil
	}

	for i := 0; i < 200; i++ {
		toAddr, err := nodeAPI.WalletNew(ctx, types.KTSecp256k1)
		if err != nil {
			log.Printf("[WARN] failed to create new wallet for destination: %v", err)
			continue
		}

		msg := &types.Message{
			To:     toAddr,
			From:   fromAddr,
			Value:  abi.NewTokenAmount(1), // small value
			Method: 0,
		}

		gasMsg, err := nodeAPI.GasEstimateMessageGas(ctx, msg, nil, types.EmptyTSK)
		if err != nil {
			log.Printf("[WARN] failed to estimate gas for message %d: %v", i, err)
			continue
		}
		msg.GasLimit = gasMsg.GasLimit
		msg.GasFeeCap = gasMsg.GasFeeCap
		msg.GasPremium = gasMsg.GasPremium

		signedMsg, err := nodeAPI.MpoolPushMessage(ctx, msg, nil)
		if err != nil {
			log.Printf("[WARN] failed to push message %d: %v", i, err)
			continue
		}
		log.Printf("[DEBUG] Pushed message %d with CID %s", i, signedMsg.Cid())
	}

	log.Printf("[INFO] Pushed 200 messages to the mempool.")
	return nil
}
