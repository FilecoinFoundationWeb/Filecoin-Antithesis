package resources

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	miner8 "github.com/filecoin-project/go-state-types/builtin/v8/miner"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
)

// findMinerBlocks searches for recent blocks mined by specified miners
func findMinerBlocks(ctx context.Context, api api.FullNode, height abi.ChainEpoch, lookbackHeight int64, minerAddrs []address.Address) map[address.Address]*types.BlockHeader {
	minerBlocks := make(map[address.Address]*types.BlockHeader)

	// Ensure we don't look back before height 0
	startHeight := height
	if startHeight < abi.ChainEpoch(lookbackHeight) {
		lookbackHeight = int64(startHeight)
	}

	for i := int64(0); i < lookbackHeight; i++ {
		currentHeight := startHeight - abi.ChainEpoch(i)
		if currentHeight < 0 {
			break
		}

		ts, err := api.ChainGetTipSetByHeight(ctx, currentHeight, types.EmptyTSK)
		if err != nil {
			log.Printf("[WARN] Failed to get tipset at height %d: %v", currentHeight, err)
			continue
		}

		for _, cid := range ts.Cids() {
			blockHeader, err := api.ChainGetBlock(ctx, cid)
			if err != nil {
				log.Printf("[WARN] Failed to get block %s: %v", cid, err)
				continue
			}

			// Check if this block is from one of our target miners
			for _, minerAddr := range minerAddrs {
				if blockHeader.Miner == minerAddr && minerBlocks[minerAddr] == nil {
					minerBlocks[minerAddr] = blockHeader
					log.Printf("[INFO] Found block from miner %s at height %d with CID %s",
						minerAddr, blockHeader.Height, cid)
				}
			}

			// If we found blocks for all miners, we can stop
			if len(minerBlocks) == len(minerAddrs) {
				return minerBlocks
			}
		}
	}

	return minerBlocks
}

func SendConsensusFault(ctx context.Context) error {
	config, err := LoadConfig("/opt/antithesis/resources/config.json")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Connect to both Lotus nodes
	api1, closer1, err := ConnectToNode(ctx, config.Nodes[0])
	if err != nil {
		return fmt.Errorf("failed to connect to lotus-1: %w", err)
	}
	defer closer1()

	api2, closer2, err := ConnectToNode(ctx, config.Nodes[1])
	if err != nil {
		return fmt.Errorf("failed to connect to lotus-2: %w", err)
	}
	defer closer2()

	// Get miner addresses from both nodes
	miner1Addr, err := api1.WalletDefaultAddress(ctx)
	if err != nil {
		return fmt.Errorf("failed to get lotus-1 miner address: %w", err)
	}

	miner2Addr, err := api2.WalletDefaultAddress(ctx)
	if err != nil {
		return fmt.Errorf("failed to get lotus-2 miner address: %w", err)
	}

	log.Printf("[INFO] Checking blocks from miners: %s, %s", miner1Addr, miner2Addr)

	// Get chain head
	head, err := api1.ChainHead(ctx)
	if err != nil {
		return fmt.Errorf("failed to get chain head: %w", err)
	}

	// Find blocks from both miners
	minerAddrs := []address.Address{miner1Addr, miner2Addr}
	minerBlocks := findMinerBlocks(ctx, api1, head.Height(), 30, minerAddrs)

	if len(minerBlocks) == 0 {
		return fmt.Errorf("could not find any recent blocks from miners in last 30 tipsets")
	}

	// Try to generate consensus faults for each miner that we found blocks for
	for minerAddr, block1 := range minerBlocks {
		log.Printf("[INFO] Generating consensus fault for miner %s using block at height %d",
			minerAddr, block1.Height)

		// Create a modified copy of the block
		block2 := *block1
		block2.ForkSignaling = block1.ForkSignaling + 1

		// Get miner info
		minfo, err := api1.StateMinerInfo(ctx, minerAddr, types.EmptyTSK)
		if err != nil {
			log.Printf("[WARN] Failed to get miner info for %s: %v", minerAddr, err)
			continue
		}

		// Sign the modified block
		signingBytes, err := block2.SigningBytes()
		if err != nil {
			log.Printf("[WARN] Failed to get signing bytes for %s: %v", minerAddr, err)
			continue
		}

		sig, err := api1.WalletSign(ctx, minfo.Worker, signingBytes)
		if err != nil {
			log.Printf("[WARN] Failed to sign block for %s: %v", minerAddr, err)
			continue
		}
		block2.BlockSig = sig

		// Marshal both blocks
		buf1 := new(bytes.Buffer)
		if err := block1.MarshalCBOR(buf1); err != nil {
			log.Printf("[WARN] Failed to marshal block1 for %s: %v", minerAddr, err)
			continue
		}

		buf2 := new(bytes.Buffer)
		if err := block2.MarshalCBOR(buf2); err != nil {
			log.Printf("[WARN] Failed to marshal block2 for %s: %v", minerAddr, err)
			continue
		}

		// Create and send consensus fault report
		params := miner8.ReportConsensusFaultParams{
			BlockHeader1: buf1.Bytes(),
			BlockHeader2: buf2.Bytes(),
		}

		sp, err := actors.SerializeParams(&params)
		if err != nil {
			log.Printf("[WARN] Failed to serialize parameters for %s: %v", minerAddr, err)
			continue
		}

		msg := &types.Message{
			From:   minfo.Worker,
			To:     minerAddr,
			Method: builtin.MethodsMiner.ReportConsensusFault,
			Params: sp,
		}

		// Log initial balance
		balance, err := api1.StateMarketBalance(ctx, minerAddr, types.EmptyTSK)
		if err != nil {
			log.Printf("[WARN] Failed to get initial balance for %s: %v", minerAddr, err)
		} else {
			log.Printf("[INFO] Initial balance for miner %s: %v", minerAddr, balance)
		}

		// Send the message
		smsg, err := api1.MpoolPushMessage(ctx, msg, nil)
		if err != nil {
			log.Printf("[WARN] Failed to push message for %s: %v", minerAddr, err)
			continue
		}

		log.Printf("[INFO] Consensus fault reported for miner %s in message %s", minerAddr, smsg.Cid())

		// Wait for message execution
		wait, err := api1.StateWaitMsg(ctx, smsg.Cid(), 5, 100, false)
		if err != nil {
			log.Printf("[WARN] Failed waiting for message for %s: %v", minerAddr, err)
			continue
		}

		if wait.Receipt.ExitCode.IsError() {
			log.Printf("[WARN] Consensus fault report failed for %s with exit code: %s",
				minerAddr, wait.Receipt.ExitCode)
			continue
		}

		// Check final balance and consensus fault status
		balanceAfter, err := api1.StateMarketBalance(ctx, minerAddr, types.EmptyTSK)
		if err != nil {
			log.Printf("[WARN] Failed to get final balance for %s: %v", minerAddr, err)
		} else {
			log.Printf("[INFO] Final balance for miner %s: %v", minerAddr, balanceAfter)
		}

		minerInfo, err := api1.StateMinerInfo(ctx, minerAddr, types.EmptyTSK)
		if err != nil {
			log.Printf("[WARN] Failed to get final miner info for %s: %v", minerAddr, err)
			continue
		}

		// Monitor consensus fault status
		for i := 0; i < 20; i++ {
			log.Printf("[INFO] Miner %s consensus fault elapsed: %v",
				minerAddr, minerInfo.ConsensusFaultElapsed)
			log.Printf("[INFO] Current balance: %v", balanceAfter)
			time.Sleep(20 * time.Second)
		}
	}

	return nil
}
