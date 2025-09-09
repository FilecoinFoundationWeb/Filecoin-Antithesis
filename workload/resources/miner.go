package resources

import (
	"bytes"
	"context"
	"log"
	"time"

	antithesisAssert "github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/exitcode"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/actors/builtin/power"
	"github.com/filecoin-project/lotus/chain/types"
	power7 "github.com/filecoin-project/specs-actors/v7/actors/builtin/power"
)

const (
	// Based on FIP-0077, miner creation is restricted before migration
	MinerCreationMigrationEpoch = 200
)

func CreateMiner(ctx context.Context, api api.FullNode, deposit abi.TokenAmount) error {
	log.Printf("[INFO] Starting miner creation...")
	head, err := api.ChainHead(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to get chain head: %v", err)
		return nil
	}
	height := head.Height()
	log.Printf("[INFO] Current chain height: %d", height)

	defaultWallet, err := api.WalletDefaultAddress(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to get default wallet address: %v", err)
		return nil
	}

	owner := defaultWallet
	worker := defaultWallet
	ssize := 2 * 1024

	// Note: the correct thing to do would be to call SealProofTypeFromSectorSize if actors version is v3 or later, but this still works
	nv, err := api.StateNetworkVersion(ctx, types.EmptyTSK)
	if err != nil {
		log.Printf("[ERROR] Failed to get network version: %v", err)
		return nil
	}

	spt, err := miner.WindowPoStProofTypeFromSectorSize(abi.SectorSize(ssize), nv)
	if err != nil {
		log.Printf("[ERROR] Failed to get post proof type: %v", err)
		return nil
	}

	params, err := actors.SerializeParams(&power7.CreateMinerParams{
		Owner:               owner,
		Worker:              worker,
		WindowPoStProofType: spt,
	})
	if err != nil {
		log.Printf("[ERROR] Failed to serialize parameters: %v", err)
		return nil
	}
	// Get required deposit for comparison
	requiredDeposit, err := api.StateMinerCreationDeposit(ctx, types.EmptyTSK)
	if err != nil {
		log.Printf("[ERROR] Failed to get required deposit: %v", err)
		return nil
	}

	createStorageMinerMsg := &types.Message{
		To:     power.Address,
		From:   worker,
		Value:  deposit,
		Method: power.Methods.CreateMiner,
		Params: params,
	}

	signed, err := api.MpoolPushMessage(ctx, createStorageMinerMsg, nil)
	if err != nil {
		log.Printf("[ERROR] Failed to push createMiner message: %v", err)
		return nil
	}

	log.Printf("[INFO] Pushed CreateMiner message: %s", signed.Cid())
	log.Printf("[INFO] Waiting for confirmation")

	mw, err := api.StateWaitMsg(ctx, signed.Cid(), 10, 20, true)
	if err != nil {
		log.Printf("[ERROR] Failed waiting for createMiner message: %v", err)
		return nil
	}

	// Handle deposit test scenarios
	switch {
	case deposit.Int.Sign() < 0:
		// Check if negative deposit was rejected
		if mw.Receipt.ExitCode == exitcode.Ok {
			log.Printf("[ERROR] Negative deposit was incorrectly accepted")
		} else {
			log.Printf("[INFO] Negative deposit correctly rejected with code %d", mw.Receipt.ExitCode)
		}

		// Antithesis assertion for monitoring
		antithesisAssert.Always(mw.Receipt.ExitCode != exitcode.Ok,
			"Miner creation must fail with negative deposit",
			map[string]interface{}{
				"operation":     "miner_creation_deposit",
				"deposit":       deposit.String(),
				"expected_code": exitcode.ErrIllegalArgument,
				"actual_code":   mw.Receipt.ExitCode,
				"requirement":   "FIP-0077 deposit validation",
				"impact":        "Critical - negative deposits must be rejected",
			})

	case deposit.IsZero():
		// Check if zero deposit was rejected
		if mw.Receipt.ExitCode == exitcode.SysErrInsufficientFunds {
			log.Printf("[INFO] Zero deposit correctly rejected with insufficient funds")
		} else {
			log.Printf("[ERROR] Zero deposit handling incorrect: got code %d, expected %d",
				mw.Receipt.ExitCode, exitcode.SysErrInsufficientFunds)
		}

		// Antithesis assertion for monitoring
		antithesisAssert.Sometimes(mw.Receipt.ExitCode == exitcode.Ok,
			"Miner creation might succeed with zero deposit if the epoch is < 200",
			map[string]interface{}{
				"operation":     "miner_creation_deposit",
				"deposit":       "0",
				"required":      requiredDeposit.String(),
				"expected_code": exitcode.SysErrInsufficientFunds,
				"actual_code":   mw.Receipt.ExitCode,
				"requirement":   "FIP-0077 deposit validation",
				"impact":        "Critical - zero deposits must be rejected",
			})

	case deposit.GreaterThan(requiredDeposit):
		// Get wallet balance before miner creation
		balanceBefore, err := api.WalletBalance(ctx, worker)
		if err != nil {
			log.Printf("[ERROR] Failed to get wallet balance before miner creation: %v", err)
			return nil
		}
		log.Printf("[INFO] Wallet balance before miner creation: %s", balanceBefore.String())

		// Check if excess deposit was accepted
		if mw.Receipt.ExitCode == exitcode.Ok {
			log.Printf("[INFO] Excess deposit correctly accepted")
		} else {
			log.Printf("[ERROR] Excess deposit incorrectly rejected with code %d", mw.Receipt.ExitCode)
		}

		// Antithesis assertion for monitoring
		antithesisAssert.Always(mw.Receipt.ExitCode == exitcode.Ok,
			"Miner creation should succeed with excess deposit",
			map[string]interface{}{
				"operation":     "miner_creation_deposit",
				"deposit":       deposit.String(),
				"required":      requiredDeposit.String(),
				"excess_amount": types.BigSub(deposit, requiredDeposit).String(),
				"expected_code": exitcode.Ok,
				"actual_code":   mw.Receipt.ExitCode,
				"requirement":   "FIP-0077 deposit handling",
				"impact":        "Critical - excess deposits should be handled correctly",
			})

		// Wait for a few blocks to ensure balance is updated
		time.Sleep(30 * time.Second)

		// Check balance after to verify excess return
		balanceAfter, err := api.WalletBalance(ctx, worker)
		if err != nil {
			log.Printf("[ERROR] Failed to get wallet balance after miner creation: %v", err)
			return nil
		}
		log.Printf("[INFO] Wallet balance after miner creation: %s", balanceAfter.String())

		// Calculate expected balance: original - required deposit (excess should be returned)
		expectedBalance := types.BigSub(balanceBefore, requiredDeposit)
		log.Printf("Required deposit: %s", requiredDeposit.String())
		log.Printf("Balance after: %s", balanceAfter.String())
		log.Printf("Expected balance: %s", expectedBalance.String())

	default:
		// Check if exact deposit was accepted
		if mw.Receipt.ExitCode == exitcode.Ok {
			log.Printf("[INFO] Exact deposit amount correctly accepted")
		} else {
			log.Printf("[ERROR] Exact deposit incorrectly rejected with code %d", mw.Receipt.ExitCode)
		}

		// Antithesis assertion for monitoring
		antithesisAssert.Always(mw.Receipt.ExitCode == exitcode.Ok,
			"Miner creation should succeed with correct deposit",
			map[string]interface{}{
				"operation":       "miner_creation",
				"current_epoch":   height,
				"migration_epoch": MinerCreationMigrationEpoch,
				"deposit":         deposit.String(),
				"required":        requiredDeposit.String(),
				"expected_code":   exitcode.Ok,
				"actual_code":     mw.Receipt.ExitCode,
				"requirement":     "FIP-0077 migration",
				"impact":          "Critical - miner creation should succeed after migration",
			})
	}

	// Process successful creation
	var retval power7.CreateMinerReturn
	if err := retval.UnmarshalCBOR(bytes.NewReader(mw.Receipt.Return)); err != nil {
		log.Printf("[ERROR] Failed to unmarshal CBOR response: %v", err)
		return nil
	}
	log.Printf("[INFO] New miners address is: %s (%s)", retval.IDAddress, retval.RobustAddress)
	log.Printf("[INFO] Miner creation completed successfully")

	return nil
}
