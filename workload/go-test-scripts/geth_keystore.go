package main

import (
	"context"
	"fmt"
	"os"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/filecoin-project/go-state-types/abi"
)

func main() {
	// Create a permanent directory for the keystore
	keystoreDir := "/opt/antithesis/resources/smart-contracts/keystore"
	if err := os.MkdirAll(keystoreDir, 0755); err != nil {
		panic(err)
	}

	// Create the keystore and generate account
	ks := keystore.NewKeyStore(keystoreDir, keystore.StandardScryptN, keystore.StandardScryptP)
	account, err := ks.NewAccount("testpassword123")
	if err != nil {
		panic(err)
	}

	// Print keystore information
	fmt.Printf("Keystore created at: %s\n", account.URL.Path)
	fmt.Printf("ETH Address: %s\n", account.Address.Hex())

	// Fund the wallet
	ctx := context.Background()
	nodeConfig := resources.NodeConfig{
		Name:          "Lotus1",
		RPCURL:        "http://lotus-1:1234/rpc/v1",
		AuthTokenPath: "/root/devgen/lotus-1/jwt",
	}
	api, closer, err := resources.ConnectToNode(ctx, nodeConfig)
	if err != nil {
		fmt.Printf("Failed to create Lotus API client: %v\n", err)
		return
	}
	defer closer()

	// Get genesis wallet to send funds from
	fromAddr, err := resources.GetGenesisWallet(ctx, api)
	if err != nil {
		fmt.Printf("Failed to get genesis wallet: %v\n", err)
		return
	}

	// Send 10000 FIL to the ETH address
	amount := abi.NewTokenAmount(1000)
	err = resources.SendFundsToEthAddress(ctx, api, fromAddr, account.Address.Hex(), amount)
	if err != nil {
		fmt.Printf("Failed to fund wallet: %v\n", err)
		return
	}
	fmt.Printf("Successfully funded wallet with 10000 FIL\n")
}
