package resources

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

// InitializeWallets creates wallets and funds them with a specified amount from the genesis wallet.
func InitializeWallets(ctx context.Context, api api.FullNode, numWallets int, fundingAmount abi.TokenAmount) error {
	genesisWallet, err := GetGenesisWallet(ctx, api)
	if err != nil {
		return fmt.Errorf("failed to get genesis wallet: %v", err)
	}

	for i := 0; i < numWallets; i++ {
		wallet, err := CreateWallet(ctx, api, types.KTSecp256k1)
		if err != nil {
			return fmt.Errorf("failed to create wallet #%d: %v", i+1, err)
		}

		err = SendFunds(ctx, api, genesisWallet, wallet, fundingAmount)
		if err != nil {
			return fmt.Errorf("failed to fund wallet #%d: %v", i+1, err)
		}

		log.Printf("Created and funded wallet #%d: %s with %s FIL", i+1, wallet, fundingAmount.String())
	}

	return nil
}

// CreateWallet creates a wallet of the specified type and returns its address.
func CreateWallet(ctx context.Context, api api.FullNode, walletType types.KeyType) (address.Address, error) {
	wallet, err := api.WalletNew(ctx, walletType)
	if err != nil {
		return address.Undef, fmt.Errorf("failed to create wallet: %w", err)
	}
	return wallet, nil
}

// SendFunds transfers FIL from the genesis wallet to a recipient wallet.
func SendFunds(ctx context.Context, api api.FullNode, from, to address.Address, amount abi.TokenAmount) error {
	msg := &types.Message{
		From:  from,
		To:    to,
		Value: amount,
	}
	sm, err := api.MpoolPushMessage(ctx, msg, nil)
	if err != nil {
		return fmt.Errorf("failed to push message to mempool: %w", err)
	}

	_, err = api.StateWaitMsg(ctx, sm.Cid(), 1, abi.ChainEpoch(-1), true)
	if err != nil {
		return fmt.Errorf("waiting for message inclusion: %w", err)
	}

	return nil
}

// GetGenesisWallet retrieves the default (genesis) wallet address.
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
		return address.Undef, fmt.Errorf("failed to list wallets: %w", err)
	}

	if len(wallets) == 0 {
		return address.Undef, fmt.Errorf("no wallets found in the node")
	}

	// Explicitly select the first wallet as fallback
	fallbackWallet := wallets[0]
	log.Printf("Using the first wallet as fallback: %s", fallbackWallet)
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
			return fmt.Errorf("failed to delete wallet %s: %w", wallet.String(), err)
		}
		log.Printf("Successfully deleted wallet: %s", wallet.String())
	}
	return nil
}
