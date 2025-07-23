package resources

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
)

// InitializeWallets creates wallets and funds them with a specified amount from the genesis wallet.
func InitializeWallets(ctx context.Context, api api.FullNode, numWallets int, fundingAmount abi.TokenAmount) error {

	genesisWallet, err := GetGenesisWallet(ctx, api)

	if err != nil {
		return fmt.Errorf("failed to get genesis wallet: %v", err)
	}

	createdWallets := 0
	for i := 0; i < numWallets; i++ {
		wallet, err := CreateWallet(ctx, api, types.KTSecp256k1)

		if err != nil {
			log.Printf("Failed to create wallet #%d: %v", i+1, err)
			continue
		}

		err = SendFunds(ctx, api, genesisWallet, wallet, fundingAmount)
		if err != nil {
			log.Printf("Failed to fund wallet #%d: %v. Wallet was created but not funded.", i+1, err)
			continue
		}

		log.Printf("Created and funded wallet #%d: %s with %s FIL", i+1, wallet, fundingAmount.String())
		createdWallets++
	}

	if createdWallets == 0 {
		return fmt.Errorf("failed to create and fund any wallets")
	}

	if createdWallets < numWallets {
		log.Printf("Warning: Only created %d out of %d requested wallets", createdWallets, numWallets)
	}

	return nil
}

// CreateWallet creates a wallet of the specified type and returns its address.
func CreateWallet(ctx context.Context, api api.FullNode, walletType types.KeyType) (address.Address, error) {
	wallet, err := api.WalletNew(ctx, walletType)
	if err != nil {
		log.Printf("Failed to create wallet: %v", err)
		return address.Undef, fmt.Errorf("failed to create wallet: %w", err)
	}
	return wallet, nil
}

// SendFunds sends funds from one address to another, waiting for the transaction to be confirmed
// It includes balance checks, message pushing to mempool, and transaction confirmation
func SendFunds(ctx context.Context, api api.FullNode, from, to address.Address, amount abi.TokenAmount) error {
	msg := &types.Message{
		From:  from,
		To:    to,
		Value: amount,
	}

	// Get balance before sending
	fromBalance, err := api.WalletBalance(ctx, from)
	if err != nil {
		log.Printf("Failed to get balance for sender %s: %v", from, err)
	} else {
		log.Printf("Sender %s balance before transfer: %s", from, fromBalance)
	}

	sm, err := api.MpoolPushMessage(ctx, msg, nil)
	if err != nil {
		assert.Sometimes(true,
			"[Message Push] Mpool push message.",
			map[string]interface{}{
				"from":           from.String(),
				"to":             to.String(),
				"error":          err.Error(),
				"value":          amount.String(),
				"from_balance":   fromBalance.String(),
				"property":       "Message pool operation",
				"impact":         "Medium - temporary mempool rejection",
				"details":        "Message push to mempool failed, may be temporary",
				"recommendation": "Check message validity and node mempool state",
			})
		log.Printf("Failed to push message to mempool: %v", err)
		return fmt.Errorf("failed to push message to mempool: %w", err)
	}
	if sm == nil {
		log.Printf("Message is nil after pushing to mempool")
		return fmt.Errorf("message is nil after pushing to mempool")
	}

	time.Sleep(20 * time.Second)

	result, err := api.StateWaitMsg(ctx, sm.Cid(), 5, abi.ChainEpoch(-1), false)
	if err != nil {
		log.Printf("Error waiting for message: %v", err)
		return fmt.Errorf("error waiting for message: %w", err)
	}

	// Check if result is nil
	if result == nil {
		log.Printf("Message result is nil")
		return fmt.Errorf("message result is nil")
	}

	// Check if the message execution was successful
	if !result.Receipt.ExitCode.IsSuccess() {
		replayResult, replayErr := api.StateReplay(ctx, types.EmptyTSK, result.Message)
		if replayErr != nil {
			log.Printf("StateReplay failed: %v", replayErr)
			return fmt.Errorf("state replay error: %w", replayErr)
		}
		if replayResult == nil {
			log.Printf("StateReplay returned nil result")
			return fmt.Errorf("state replay returned nil result")
		}
		return fmt.Errorf("message execution failed with exit code: %d", result.Receipt.ExitCode)
	}

	return nil
}

// GetGenesisWallet retrieves the default (genesis) wallet address
// If no default wallet is set, it falls back to the first wallet in the list
func GetGenesisWallet(ctx context.Context, api api.FullNode) (address.Address, error) {
	// Attempt to get the default wallet
	genesisWallet, err := api.WalletDefaultAddress(ctx)

	if err == nil && genesisWallet != address.Undef {
		log.Printf("Default wallet found: %s", genesisWallet)
		return genesisWallet, nil
	}

	// Log the absence of a default wallet
	if err != nil {
		log.Printf("Error fetching default wallet: %v", err)
	} else {
		log.Println("No default wallet set.")
	}

	// Fallback: List all wallets
	wallets, err := api.WalletList(ctx)
	if err != nil {
		log.Printf("Failed to list wallets: %v", err)
		return address.Undef, fmt.Errorf("failed to list wallets: %w", err)
	}

	if len(wallets) == 0 {
		log.Printf("No wallets found in the node")
		return address.Undef, fmt.Errorf("no wallets found in the node")
	}

	// Explicitly select the first wallet as fallback
	fallbackWallet := wallets[0]
	log.Printf("Using the first wallet as fallback: %s", fallbackWallet)

	if fallbackWallet == address.Undef {
		return address.Undef, fmt.Errorf("invalid fallback wallet address")
	}

	return fallbackWallet, nil
}

// GetAllWalletAddressesExceptGenesis retrieves all wallet addresses except the genesis wallet.
func GetAllWalletAddressesExceptGenesis(ctx context.Context, api api.FullNode) ([]address.Address, error) {
	genesisWallet, err := GetGenesisWallet(ctx, api)
	if err != nil {
		return nil, fmt.Errorf("failed to get genesis wallet: %w", err)
	}

	allWallets, err := api.WalletList(ctx)
	if err != nil {
		log.Printf("Failed to list wallets: %v", err)
		return nil, fmt.Errorf("failed to list wallets: %w", err)
	}

	var walletsToDelete []address.Address
	for _, wallet := range allWallets {
		if wallet != genesisWallet {
			walletsToDelete = append(walletsToDelete, wallet)
		}
	}

	return walletsToDelete, nil
}

// GetRandomWallets selects a random subset of wallets to delete.
func GetRandomWallets(ctx context.Context, api api.FullNode, numWallets int) ([]address.Address, error) {
	allWallets, err := GetAllWalletAddressesExceptGenesis(ctx, api)
	if err != nil {
		return nil, fmt.Errorf("failed to list wallets: %w", err)
	}

	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(allWallets), func(i, j int) { allWallets[i], allWallets[j] = allWallets[j], allWallets[i] })

	if len(allWallets) < numWallets {
		log.Printf("Only %d wallets available. Selecting all.", len(allWallets))
		numWallets = len(allWallets)
	}

	return allWallets[:numWallets], nil
}

// DeleteWallets deletes the specified wallets from the Lotus node.
func DeleteWallets(ctx context.Context, api api.FullNode, walletsToDelete []address.Address) error {
	for _, wallet := range walletsToDelete {
		err := api.WalletDelete(ctx, wallet)
		if err != nil {
			log.Printf("Failed to delete wallet %s: %v", wallet.String(), err)
			return fmt.Errorf("failed to delete wallet %s: %w", wallet.String(), err)
		}
		log.Printf("Successfully deleted wallet: %s", wallet.String())
	}
	return nil
}

// SendFundsToEthAddress sends funds from a Filecoin address to an ETH address
// It handles address conversion and transaction creation
func SendFundsToEthAddress(ctx context.Context, api api.FullNode, from address.Address, ethAddr string) error {
	// Remove 0x prefix if present
	ea, err := ethtypes.ParseEthAddress(ethAddr)
	if err != nil {
		return fmt.Errorf("failed to parse target address; address must be a valid FIL address or an ETH address: %w", err)
	}
	fmt.Printf("ea: %s\n", ea)
	// Convert to f4 address
	to, err := ea.ToFilecoinAddress()
	if err != nil {
		return fmt.Errorf("failed to convert eth address to filecoin address: %w", err)
	}
	fmt.Printf("to: %s\n", to)
	// Create message
	amountFIL, err := types.ParseFIL("1000")
	if err != nil {
		return fmt.Errorf("failed to parse amount: %w", err)
	}
	msg := &types.Message{
		From:       from,
		To:         to,
		Value:      abi.TokenAmount(amountFIL),
		Method:     builtin.MethodsEAM.CreateExternal,
		Params:     nil,
		GasLimit:   0,
		GasFeeCap:  abi.NewTokenAmount(0),
		GasPremium: abi.NewTokenAmount(0),
	}

	// Push message to mempool with automatic gas estimation
	sm, err := api.MpoolPushMessage(ctx, msg, nil)
	if err != nil {
		assert.Sometimes(true,
			"[Message Push] Mpool push message to ETH address.",
			map[string]interface{}{
				"from":           from.String(),
				"to":             to.String(),
				"eth_address":    ethAddr,
				"error":          err.Error(),
				"value":          amountFIL.String(),
				"property":       "Message pool operation",
				"impact":         "Medium - temporary mempool rejection",
				"details":        "Message push to mempool failed, may be temporary",
				"recommendation": "Check message validity and node mempool state",
			})
		log.Printf("Failed to push message to mempool: %v", err)
		return fmt.Errorf("failed to push message to mempool: %w", err)
	}

	if sm == nil {
		log.Printf("Message is nil after pushing to mempool")
		return fmt.Errorf("message is nil after pushing to mempool")
	}

	// Wait for message execution
	time.Sleep(20 * time.Second)

	result, err := api.StateWaitMsg(ctx, sm.Cid(), 5, abi.ChainEpoch(-1), false)
	if err != nil {
		log.Printf("Error waiting for message: %v", err)
		return fmt.Errorf("error waiting for message: %w", err)
	}

	// Check if result is nil
	if result == nil {
		log.Printf("Message result is nil")
		return fmt.Errorf("message result is nil")
	}

	// Check if the message execution was successful
	if !result.Receipt.ExitCode.IsSuccess() {
		replayResult, replayErr := api.StateReplay(ctx, types.EmptyTSK, result.Message)
		if replayErr != nil {
			log.Printf("StateReplay failed: %v", replayErr)
			return fmt.Errorf("state replay error: %w", replayErr)
		}
		if replayResult == nil {
			log.Printf("StateReplay returned nil result")
			return fmt.Errorf("state replay returned nil result")
		}
		return fmt.Errorf("message execution failed with exit code: %d", result.Receipt.ExitCode)
	}

	return nil
}
