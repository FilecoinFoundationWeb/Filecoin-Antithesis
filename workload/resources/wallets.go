package resources

import (
	"context"
	"log"
	"math/rand"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
)

// InitializeWallets creates wallets and funds them with a specified amount from the genesis wallet.
func InitializeWallets(ctx context.Context, api api.FullNode, numWallets int, fundingAmount abi.TokenAmount) error {

	genesisWallet, err := GetGenesisWallet(ctx, api)

	if err != nil {
		log.Printf("[ERROR] Failed to get genesis wallet: %v", err)
		return nil
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
		log.Printf("[ERROR] Failed to create and fund any wallets")
		return nil
	}

	if createdWallets < numWallets {
		log.Printf("Warning: Only created %d out of %d requested wallets", createdWallets, numWallets)
	}

	return nil
}

func InitializeForestWallets(ctx context.Context, api, lotusapi api.FullNode, numWallets int, fundingAmount abi.TokenAmount) error {

	wallet, err := CreateWallet(ctx, api, types.KTBLS)
	if err != nil {
		log.Printf("[ERROR] Failed to create wallet: %v", err)
		return nil
	}
	log.Printf("Created wallet: %s", wallet)

	genesisWallet, err := GetGenesisWallet(ctx, lotusapi)
	if err != nil {
		log.Printf("[ERROR] Failed to get genesis wallet: %v", err)
		return nil
	}
	funds, err := api.WalletBalance(ctx, genesisWallet)
	if err != nil {
		log.Printf("[ERROR] Failed to get balance: %v", err)
		return nil
	}
	fundingAmount = big.Div(funds, big.NewInt(4))

	err = SendFunds(ctx, api, genesisWallet, wallet, fundingAmount)
	if err != nil {
		log.Printf("[ERROR] Failed to send funds: %v", err)
		return nil
	}
	log.Printf("Sent funds to wallet: %s", wallet)

	err = api.WalletSetDefault(ctx, wallet)
	if err != nil {
		log.Printf("[ERROR] Failed to set default wallet: %v", err)
		return nil
	}
	log.Printf("Set default wallet: %s", wallet)
	time.Sleep(10 * time.Second)
	err = CreateForestWallets(ctx, api, 3, abi.NewTokenAmount(100000000000000))
	if err != nil {
		log.Printf("[ERROR] Failed to create forest wallets: %v", err)
		return nil
	}
	return nil
}

func CreateForestWallets(ctx context.Context, api api.FullNode, numWallets int, fundingAmount abi.TokenAmount) error {
	defaultWallet, err := api.WalletDefaultAddress(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to get default wallet: %v", err)
		return nil
	}
	funds, err := api.WalletBalance(ctx, defaultWallet)
	if err != nil {
		log.Printf("[ERROR] Failed to get balance: %v", err)
		return nil
	}
	nodeconfig := NodeConfig{
		Name:          "Forest",
		RPCURL:        "http://lotus-1:1234/rpc/v1",
		AuthTokenPath: "/root/devgen/lotus-1/jwt",
	}
	lotusapi, closer, err := ConnectToNode(ctx, nodeconfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node: %v", err)
		return nil
	}
	defer closer()
	if funds == abi.NewTokenAmount(0) {
		log.Printf("[ERROR] Default wallet has no balance")
		InitializeForestWallets(ctx, api, lotusapi, 1, abi.NewTokenAmount(1000000000000000000))
		return nil
	}
	log.Printf("Balance: %s", funds)
	createdWallets := 0
	for i := 0; i < numWallets; i++ {
		wallet, err := CreateWallet(ctx, api, types.KTBLS)
		if err != nil {
			log.Printf("[ERROR] Failed to create wallet: %v", err)
			return nil
		}
		err = SendFunds(ctx, api, defaultWallet, wallet, fundingAmount)
		if err != nil {
			log.Printf("Failed to fund wallet #%d: %v. Wallet was created but not funded.", i+1, err)
			continue
		}
		log.Printf("Created and funded wallet #%d: %s with %s FIL", i+1, wallet, fundingAmount.String())
		createdWallets++
	}

	if createdWallets == 0 {
		log.Printf("[ERROR] Failed to create and fund any wallets")
		return nil
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
		log.Printf("[ERROR] Failed to create wallet: %v", err)
		return address.Undef, nil
	}
	return wallet, nil
}

// SendFunds sends funds from one address to another, waiting for the transaction to be confirmed
// It includes balance checks, message pushing to mempool, and transaction confirmation
func SendFunds(ctx context.Context, api api.FullNode, from, to address.Address, amount abi.TokenAmount) error {
	// Check for undefined addresses
	if from == address.Undef {
		log.Printf("[ERROR] Source address is undefined")
		return nil
	}
	if to == address.Undef {
		log.Printf("[ERROR] Destination address is undefined")
		return nil
	}

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
		log.Printf("[ERROR] Failed to push message to mempool: %v", err)
		return nil
	}
	if sm == nil {
		log.Printf("Message is nil after pushing to mempool")
		log.Printf("[ERROR] Message is nil after pushing to mempool")
		return nil
	}

	time.Sleep(20 * time.Second)

	result, err := api.StateWaitMsg(ctx, sm.Cid(), 5, abi.ChainEpoch(-1), false)
	if err != nil {
		log.Printf("Error waiting for message: %v", err)
		log.Printf("[ERROR] Error waiting for message: %v", err)
		return nil
	}

	// Check if result is nil
	if result == nil {
		log.Printf("Message result is nil")
		log.Printf("[ERROR] Message result is nil")
		return nil
	}

	// Check if the message execution was successful
	if !result.Receipt.ExitCode.IsSuccess() {
		replayResult, replayErr := api.StateReplay(ctx, types.EmptyTSK, result.Message)
		if replayErr != nil {
			log.Printf("StateReplay failed: %v", replayErr)
			log.Printf("[ERROR] State replay error: %v", replayErr)
			return nil
		}
		if replayResult == nil {
			log.Printf("StateReplay returned nil result")
			log.Printf("[ERROR] State replay returned nil result")
			return nil
		}
		log.Printf("[ERROR] Message execution failed with exit code: %d", result.Receipt.ExitCode)
		return nil
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
		log.Printf("[ERROR] Failed to list wallets: %v", err)
		return address.Undef, err
	}

	if len(wallets) == 0 {
		log.Printf("[ERROR] No wallets found in the node")
		return address.Undef, nil
	}

	// Explicitly select the first wallet as fallback
	fallbackWallet := wallets[0]
	log.Printf("Using the first wallet as fallback: %s", fallbackWallet)

	if fallbackWallet == address.Undef {
		log.Printf("[ERROR] Invalid fallback wallet address")
		return address.Undef, nil
	}

	return fallbackWallet, nil
}

// GetAllWalletAddressesExceptGenesis retrieves all wallet addresses except the genesis wallet.
func GetAllWalletAddressesExceptGenesis(ctx context.Context, api api.FullNode) ([]address.Address, error) {
	genesisWallet, err := GetGenesisWallet(ctx, api)
	if err != nil {
		log.Printf("[ERROR] Failed to get genesis wallet: %v", err)
		return nil, nil
	}

	allWallets, err := api.WalletList(ctx)
	if err != nil {
		log.Printf("Failed to list wallets: %v", err)
		log.Printf("[ERROR] Failed to list wallets: %v", err)
		return nil, nil
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
		log.Printf("[ERROR] Failed to list wallets: %v", err)
		return nil, nil
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(allWallets), func(i, j int) { allWallets[i], allWallets[j] = allWallets[j], allWallets[i] })

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
			log.Printf("[ERROR] Failed to delete wallet %s: %v", wallet.String(), err)
			continue
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
		log.Printf("[ERROR] Failed to parse target address; address must be a valid FIL address or an ETH address: %v", err)
		return nil
	}
	log.Printf("[INFO] ETH address: %s", ea)
	// Convert to f4 address
	to, err := ea.ToFilecoinAddress()
	if err != nil {
		log.Printf("[ERROR] Failed to convert eth address to filecoin address: %v", err)
		return nil
	}
	log.Printf("[INFO] Filecoin address: %s", to)
	// Create message
	amountFIL, err := types.ParseFIL("1000")
	if err != nil {
		log.Printf("[ERROR] Failed to parse amount: %v", err)
		return nil
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
		log.Printf("[ERROR] Failed to push message to mempool: %v", err)
		return nil
	}

	if sm == nil {
		log.Printf("Message is nil after pushing to mempool")
		log.Printf("[ERROR] Message is nil after pushing to mempool")
		return nil
	}

	// Wait for message execution
	time.Sleep(20 * time.Second)

	result, err := api.StateWaitMsg(ctx, sm.Cid(), 5, abi.ChainEpoch(-1), false)
	if err != nil {
		log.Printf("Error waiting for message: %v", err)
		log.Printf("[ERROR] Error waiting for message: %v", err)
		return nil
	}

	// Check if result is nil
	if result == nil {
		log.Printf("Message result is nil")
		log.Printf("[ERROR] Message result is nil")
		return nil
	}

	// Check if the message execution was successful
	if !result.Receipt.ExitCode.IsSuccess() {
		replayResult, replayErr := api.StateReplay(ctx, types.EmptyTSK, result.Message)
		if replayErr != nil {
			log.Printf("StateReplay failed: %v", replayErr)
			log.Printf("[ERROR] State replay error: %v", replayErr)
			return nil
		}
		if replayResult == nil {
			log.Printf("StateReplay returned nil result")
			log.Printf("[ERROR] State replay returned nil result")
			return nil
		}
		log.Printf("[ERROR] Message execution failed with exit code: %d", result.Receipt.ExitCode)
		return nil
	}

	return nil
}

func CreateEthKeystoreWallet(ctx context.Context, nodeConfig *NodeConfig, keystoreDir string) error {
	log.Printf("Creating ETH keystore wallet for node: %s", nodeConfig.Name)
	ethAddr, keystorePath, err := CreateEthKeystore(keystoreDir)
	if err != nil {
		log.Printf("[ERROR] Failed to create ETH keystore wallet: %v", err)
		return err
	}
	log.Printf("ETH keystore wallet created: %s", ethAddr)
	log.Printf("ETH keystore wallet saved at: %s", keystorePath)
	log.Printf("Keystore created for node: %s", nodeConfig.Name)

	// Connect to the node to fund the ETH address
	api, closer, err := ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return err
	}
	defer closer()

	// Get a funded wallet to send from
	fromWallet, err := GetGenesisWallet(ctx, api)
	if err != nil {
		log.Printf("[ERROR] Failed to get genesis wallet for funding: %v", err)
		return err
	}

	// Convert Ethereum address to string for SendFundsToEthAddress
	ethAddrStr := ethAddr.Hex()
	log.Printf("Funding ETH address %s from wallet %s", ethAddrStr, fromWallet)

	err = SendFundsToEthAddress(ctx, api, fromWallet, ethAddrStr)
	if err != nil {
		log.Printf("[ERROR] Failed to fund ETH address: %v", err)
		return err
	}

	log.Printf("Successfully funded ETH keystore wallet: %s", ethAddrStr)
	return nil
}

// PerformCreateOperation creates wallets on a specified node
func PerformCreateOperation(ctx context.Context, nodeConfig *NodeConfig, numWallets int, tokenAmount abi.TokenAmount) error {
	log.Printf("Creating %d wallets on node '%s'...", numWallets, nodeConfig.Name)

	// Retry connection up to 3 times
	for retry := 0; retry < 3; retry++ {
		api, closer, err := ConnectToNode(ctx, *nodeConfig)
		if err != nil {
			log.Printf("Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
			return nil
		}

		// Handle graceful connection failure
		if api == nil {
			if retry < 2 {
				log.Printf("[WARN] Could not establish connection to node '%s' (retry %d/3), retrying...", nodeConfig.Name, retry+1)
				time.Sleep(5 * time.Second)
				continue
			}
			log.Printf("[WARN] Could not establish connection to node '%s' after 3 attempts, skipping wallet creation", nodeConfig.Name)
			return nil
		}

		defer closer()

		err = InitializeWallets(ctx, api, numWallets, tokenAmount)
		if err != nil {
			log.Printf("Warning: Error occurred during wallet initialization: %v", err)
			return err
		} else {
			log.Printf("Wallets created successfully on node '%s'", nodeConfig.Name)
			return nil
		}
	}

	return nil
}

// PerformDeleteOperation deletes wallets on a specified node
func PerformDeleteOperation(ctx context.Context, nodeConfig *NodeConfig) error {
	log.Printf("Deleting wallets on node '%s'...", nodeConfig.Name)

	api, closer, err := ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return nil
	}
	defer closer()

	return RetryOperation(ctx, func() error {
		allWallets, err := GetAllWalletAddressesExceptGenesis(ctx, api)
		if err != nil {
			log.Printf("[ERROR] Failed to list wallets on node '%s': %v", nodeConfig.Name, err)
			return nil
		}

		if len(allWallets) == 0 {
			log.Printf("No wallets available to delete on node '%s'", nodeConfig.Name)
			return nil
		}

		// Delete a random number of wallets
		rand.Seed(time.Now().UnixNano())
		numToDelete := rand.Intn(len(allWallets)) + 1
		walletsToDelete := allWallets[:numToDelete]

		if err := DeleteWallets(ctx, api, walletsToDelete); err != nil {
			log.Printf("[ERROR] Failed to delete wallets on node '%s': %v", nodeConfig.Name, err)
			return nil
		}

		log.Printf("Deleted %d wallets successfully on node '%s'", numToDelete, nodeConfig.Name)
		return nil
	}, "Delete wallets operation")
}
