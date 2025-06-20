package resources

import (
	"bytes"
	"context"
	"fmt"
	"log"

	"github.com/filecoin-project/go-state-types/builtin"
	miner8 "github.com/filecoin-project/go-state-types/builtin/v8/miner"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
)

func SendConsensusFault(ctx context.Context) error {
	config, err := LoadConfig("/opt/antithesis/resources/config.json")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Try with first miner
	api1, closer1, err := ConnectToNode(ctx, config.Nodes[0])
	if err != nil {
		return fmt.Errorf("failed to connect to lotus-1: %w", err)
	}
	defer closer1()

	// Get chain head
	head, err := api1.ChainHead(ctx)
	if err != nil {
		return fmt.Errorf("failed to get chain head: %w", err)
	}

	// Try head-1 tipset
	ts, err := api1.ChainGetTipSetByHeight(ctx, head.Height()-2, types.EmptyTSK)
	if err != nil {
		return fmt.Errorf("failed to get tipset: %w", err)
	}

	// Try with first block from tipset
	blockHeader, err := api1.ChainGetBlock(ctx, ts.Cids()[0])
	if err != nil {
		return fmt.Errorf("getting block header: %w", err)
	}

	// Get miner info
	minfo, err := api1.StateMinerInfo(ctx, blockHeader.Miner, types.EmptyTSK)
	if err != nil {
		log.Printf("[WARN] Failed with miner1, trying miner2: %v", err)
	} else {
		// Create modified copy with different fork signaling
		blockHeaderCopy := *blockHeader
		blockHeaderCopy.ForkSignaling = blockHeader.ForkSignaling + 1

		// Sign the modified block
		signingBytes, err := blockHeaderCopy.SigningBytes()
		if err == nil {
			sig, err := api1.WalletSign(ctx, minfo.Worker, signingBytes)
			if err == nil {
				blockHeaderCopy.BlockSig = sig

				// Marshal both blocks
				buf1 := new(bytes.Buffer)
				buf2 := new(bytes.Buffer)
				err1 := blockHeader.MarshalCBOR(buf1)
				err2 := blockHeaderCopy.MarshalCBOR(buf2)
				if err1 == nil && err2 == nil {
					// Create consensus fault params
					params := miner8.ReportConsensusFaultParams{
						BlockHeader1: buf1.Bytes(),
						BlockHeader2: buf2.Bytes(),
					}

					sp, err := actors.SerializeParams(&params)
					if err == nil {
						// Send the message
						smsg, err := api1.MpoolPushMessage(ctx, &types.Message{
							From:   minfo.Worker,
							To:     blockHeader.Miner,
							Method: builtin.MethodsMiner.ReportConsensusFault,
							Params: sp,
						}, nil)
						if err == nil {
							log.Printf("Consensus fault reported in message %s", smsg.Cid())

							// Wait for message execution
							wait, err := api1.StateWaitMsg(ctx, smsg.Cid(), 5, api.LookbackNoLimit, false)
							if err == nil && !wait.Receipt.ExitCode.IsError() {
								return nil // Success with first miner
							}
						}
					}
				}
			}
		}
		log.Printf("[WARN] Failed with miner1, trying miner2")
	}

	// Try with second miner
	api2, closer2, err := ConnectToNode(ctx, config.Nodes[1])
	if err != nil {
		return fmt.Errorf("failed to connect to lotus-2: %w", err)
	}
	defer closer2()

	// Get chain head
	head, err = api2.ChainHead(ctx)
	if err != nil {
		return fmt.Errorf("failed to get chain head: %w", err)
	}

	// Try head-1 tipset
	ts, err = api2.ChainGetTipSetByHeight(ctx, head.Height()-2, types.EmptyTSK)
	if err != nil {
		return fmt.Errorf("failed to get tipset: %w", err)
	}

	// Try with first block from tipset
	blockHeader, err = api2.ChainGetBlock(ctx, ts.Cids()[0])
	if err != nil {
		return fmt.Errorf("getting block header: %w", err)
	}

	// Get miner info
	minfo, err = api2.StateMinerInfo(ctx, blockHeader.Miner, types.EmptyTSK)
	if err != nil {
		return fmt.Errorf("getting miner info: %w", err)
	}

	// Create modified copy with different fork signaling
	blockHeaderCopy := *blockHeader
	blockHeaderCopy.ForkSignaling = blockHeader.ForkSignaling + 1

	// Sign the modified block
	signingBytes, err := blockHeaderCopy.SigningBytes()
	if err != nil {
		return fmt.Errorf("getting signing bytes: %w", err)
	}

	sig, err := api2.WalletSign(ctx, minfo.Worker, signingBytes)
	if err != nil {
		log.Printf("Could not sign block with miner 2: %v. This may be expected, aborting fault attempt.", err)
		return nil
	}
	blockHeaderCopy.BlockSig = sig

	// Marshal both blocks
	buf1 := new(bytes.Buffer)
	buf2 := new(bytes.Buffer)
	if err := blockHeader.MarshalCBOR(buf1); err != nil {
		return fmt.Errorf("marshalling block1: %w", err)
	}
	if err := blockHeaderCopy.MarshalCBOR(buf2); err != nil {
		return fmt.Errorf("marshalling block2: %w", err)
	}

	// Create consensus fault params
	params := miner8.ReportConsensusFaultParams{
		BlockHeader1: buf1.Bytes(),
		BlockHeader2: buf2.Bytes(),
	}

	sp, err := actors.SerializeParams(&params)
	if err != nil {
		return fmt.Errorf("serializing params: %w", err)
	}

	// Send the message
	smsg, err := api2.MpoolPushMessage(ctx, &types.Message{
		From:   minfo.Worker,
		To:     blockHeader.Miner,
		Method: builtin.MethodsMiner.ReportConsensusFault,
		Params: sp,
	}, nil)
	if err != nil {
		return fmt.Errorf("mpool push: %w", err)
	}

	log.Printf("Consensus fault reported in message %s", smsg.Cid())

	// Wait for message execution
	wait, err := api2.StateWaitMsg(ctx, smsg.Cid(), 5, api.LookbackNoLimit, false)
	if err != nil {
		return fmt.Errorf("waiting for message: %w", err)
	}

	if wait.Receipt.ExitCode.IsError() {
		return fmt.Errorf("consensus fault failed with exit code: %s", wait.Receipt.ExitCode)
	}

	return nil
}
