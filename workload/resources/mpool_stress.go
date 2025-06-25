package resources

import (
	"context"
	"crypto/rand"
	"fmt"
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
		return fmt.Errorf("failed to get default wallet: %w", err)
	}

	toAddr, err := nodeAPI.WalletNew(ctx, types.KTSecp256k1)
	if err != nil {
		return fmt.Errorf("failed to create new wallet for destination: %w", err)
	}
	log.Printf("[INFO] Sending max sized message from %s to %s", fromAddr, toAddr)

	params := make([]byte, MaxMessageSize)
	if _, err := rand.Read(params); err != nil {
		return fmt.Errorf("failed to generate random params: %w", err)
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
		return fmt.Errorf("failed to estimate gas: %w", err)
	}
	msg.GasLimit = gasMsg.GasLimit
	msg.GasFeeCap = gasMsg.GasFeeCap
	msg.GasPremium = gasMsg.GasPremium

	log.Printf("[INFO] Pushing message with size %d bytes to mempool", len(params))
	signedMsg, err := nodeAPI.MpoolPushMessage(ctx, msg, nil)
	if err != nil {
		return fmt.Errorf("failed to push max-sized message: %w", err)
	}

	log.Printf("[INFO] Max-sized message sent successfully, CID: %s", signedMsg.Cid())
	return nil
}

func SendMaxMessages(ctx context.Context, nodeAPI api.FullNode) error {
	log.Println("[INFO] Starting test for maximum messages in a block...")

	fromAddr, err := nodeAPI.WalletDefaultAddress(ctx)
	if err != nil {
		return fmt.Errorf("failed to get default wallet: %w", err)
	}

	for i := 0; i < MaxMessagesPerBlock; i++ {
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

	log.Printf("[INFO] Pushed %d messages to the mempool.", MaxMessagesPerBlock)
	return nil
}
