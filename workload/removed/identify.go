package main

import (
	"context"
	"fmt"
	"log"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
)

func main() {
	ctx := context.Background()

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Find Lotus1 node config
	var nodeConfig *resources.NodeConfig
	for _, node := range config.Nodes {
		if node.Name == "Lotus1" {
			nodeConfig = &node
			break
		}
	}
	if nodeConfig == nil {
		log.Fatal("Lotus1 node not found in config")
	}

	// Connect to the Lotus node
	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Fatalf("Failed to connect to Lotus node: %v", err)
	}
	defer closer()

	// Get default address from the node
	fromAddress, err := api.WalletDefaultAddress(ctx)
	if err != nil {
		log.Fatalf("Failed to get default address: %v", err)
	}

	// Create a new Ethereum account
	key, ethAddr, delegatedAddr := resources.NewAccount()
	log.Printf("Created new account: ETH=%s, FIL=%s", ethAddr.String(), delegatedAddr.String())

	// Send some funds to initialize the delegated address
	err = resources.SendFunds(ctx, api, fromAddress, delegatedAddr, types.FromFil(1))
	if err != nil {
		log.Fatalf("Failed to send initial funds: %v", err)
	}
	log.Printf("Sent 1 FIL to initialize delegated address")

	// Get nonce from the node
	nonce, err := api.MpoolGetNonce(ctx, delegatedAddr)
	if err != nil {
		log.Fatalf("Failed to get nonce: %v", err)
	}

	// Get gas parameters from node
	maxPriorityFeePerGas, err := api.EthMaxPriorityFeePerGas(ctx)
	if err != nil {
		log.Fatalf("Failed to get max priority fee: %v", err)
	}

	// Create transaction
	tx := ethtypes.Eth1559TxArgs{
		ChainID:              31415926,
		To:                   &ethAddr, // Sending to same address for testing
		Value:                big.Zero(),
		Nonce:                int(nonce),
		MaxFeePerGas:         types.NanoFil,
		MaxPriorityFeePerGas: big.Int(maxPriorityFeePerGas),
		GasLimit:             21000,
		Input:                []byte{},
		V:                    big.Zero(),
		R:                    big.Zero(),
		S:                    big.Zero(),
	}

	// Sign transaction with the new account's private key
	resources.SignTransaction(&tx, key.PrivateKey)

	// Submit transaction
	err = resources.SubmitTransaction(ctx, api, &tx)
	if err != nil {
		log.Fatalf("Failed to submit transaction: %v", err)
	}

	fmt.Printf("Transaction submitted successfully\n")
}
