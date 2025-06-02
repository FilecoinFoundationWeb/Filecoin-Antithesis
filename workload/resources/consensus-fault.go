package resources

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"time"

	"github.com/filecoin-project/go-state-types/builtin"
	miner8 "github.com/filecoin-project/go-state-types/builtin/v8/miner"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
	"golang.org/x/xerrors"
)

func SendConsensusFault(ctx context.Context) error {
	config, err := LoadConfig("/opt/antithesis/resources/config.json")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	api, closer, err := ConnectToNode(ctx, config.Nodes[0])
	if err != nil {
		return fmt.Errorf("failed to connect to node: %w", err)
	}
	defer closer()

	// Get current head
	head, err := api.ChainHead(ctx)
	if err != nil {
		return fmt.Errorf("failed to get chain head: %w", err)
	}

	ts, err := api.ChainGetTipSetByHeight(ctx, head.Height()-2, head.Key())
	if err != nil {
		return fmt.Errorf("failed to get tipset at height %d: %w", head.Height()-2, err)
	}

	if len(ts.Blocks()) == 0 {
		return fmt.Errorf("no blocks found in tipset at height %d", head.Height()-2)
	}

	// Get the first block
	block1 := ts.Blocks()[0]

	// Create a modified copy of the block
	block2 := *block1
	block2.ForkSignaling = block1.ForkSignaling + 1

	// Get miner info for the block's miner
	maddr := block1.Miner
	minfo, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	fmt.Printf("Minfo %v", minfo)
	if err != nil {
		return fmt.Errorf("failed to get miner info: %w", err)
	}

	// Sign the modified block
	signingBytes, err := block2.SigningBytes()
	if err != nil {
		return fmt.Errorf("failed to get signing bytes: %w", err)
	}
	log.Printf("Minfo worker %s", minfo.Worker)
	sig, err := api.WalletSign(ctx, minfo.Worker, signingBytes)
	if err != nil {
		time.Sleep(10 * time.Second)
		return fmt.Errorf("failed to sign block: %w", err)
	}
	block2.BlockSig = sig

	buf1 := new(bytes.Buffer)
	if err := block1.MarshalCBOR(buf1); err != nil {
		return fmt.Errorf("failed to marshal block1: %w", err)
	}

	buf2 := new(bytes.Buffer)
	if err := block2.MarshalCBOR(buf2); err != nil {
		return fmt.Errorf("failed to marshal block2: %w", err)
	}

	params := miner8.ReportConsensusFaultParams{
		BlockHeader1: buf1.Bytes(),
		BlockHeader2: buf2.Bytes(),
	}

	sp, err := actors.SerializeParams(&params)
	if err != nil {
		return fmt.Errorf("failed to serialize parameters: %w", err)
	}

	msg := &types.Message{
		From:   minfo.Worker,
		To:     maddr,
		Method: builtin.MethodsMiner.ReportConsensusFault,
		Params: sp,
	}
	balance, err := api.StateMarketBalance(ctx, maddr, types.EmptyTSK)
	if err != nil {
		log.Println(err)
	}
	log.Printf("Balance before: %v", balance)
	smsg, err := api.MpoolPushMessage(ctx, msg, nil)
	if err != nil {
		return xerrors.Errorf("mpool push failed: %w", err)
	}

	wait, err := api.StateWaitMsg(ctx, smsg.Cid(), 5, 100, false)
	if err != nil {
		return fmt.Errorf("failed waiting for message: %w", err)
	}

	if wait.Receipt.ExitCode.IsError() {
		return fmt.Errorf("consensus fault report failed with exit code: %s", wait.Receipt.ExitCode)
	}
	balanceAfter, err := api.StateMarketBalance(ctx, maddr, types.EmptyTSK)
	if err != nil {
		log.Println(err)
	}

	minerinfo, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return fmt.Errorf("failed to get miner info: %w", err)
	}
	i := 0
	for i <= 20 {
		log.Printf("Minfo worker %s", minerinfo.ConsensusFaultElapsed)
		log.Printf("Balance after: %v", balanceAfter)
		time.Sleep(20 * time.Second)
		i++
	}
	return nil
}
