package main

import (
	"context"
	"flag"
	"log"
	"math/big"
	"math/rand"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

func main() {
	ctx := context.Background()

	// CLI flags
	configFile := flag.String("config", "/opt/antithesis/resources/config.json", "Path to config JSON file")
	operation := flag.String("operation", "", "Operation: 'create', 'delete', 'spam', 'connect', 'disconnect', or 'deploy'")
	nodeName := flag.String("node", "", "Node name from config.json (required for certain operations)")
	numWallets := flag.Int("wallets", 1, "Number of wallets for the operation (required for 'create' and 'delete')")
	contractPath := flag.String("contract", "", "Path to the smart contract bytecode file (required for 'deploy')")

	flag.Parse()

	// Validate inputs
	if *operation != "create" && *operation != "delete" && *operation != "spam" && *operation != "connect" && *operation != "disconnect" && *operation != "deploy" {
		log.Fatalf("Invalid operation: %s. Use 'create', 'delete', 'spam', 'connect', 'disconnect', or 'deploy'.", *operation)
	}
	if (*operation == "create" || *operation == "delete" || *operation == "connect" || *operation == "disconnect" || *operation == "deploy") && *nodeName == "" {
		log.Fatalf("Node name is required for the '%s' operation.", *operation)
	}
	if *operation == "deploy" && *contractPath == "" {
		log.Fatalf("Contract path is required for the 'deploy' operation.")
	}

	// Load configuration
	config, err := resources.LoadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Select node configuration
	var nodeConfig *resources.NodeConfig
	for _, node := range config.Nodes {
		if node.Name == *nodeName {
			nodeConfig = &node
			break
		}
	}
	if (*operation == "create" || *operation == "delete" || *operation == "connect" || *operation == "disconnect" || *operation == "deploy") && nodeConfig == nil {
		log.Fatalf("Node '%s' not found in config.json.", *nodeName)
	}

	// Perform operations
	fundingAmount, _ := new(big.Int).SetString(config.DefaultFundingAmount, 10)
	tokenAmount := abi.TokenAmount(types.BigInt{Int: fundingAmount})

	switch *operation {
	case "create":
		log.Printf("Creating %d wallets on node '%s'...", *numWallets, *nodeName)
		api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
		if err != nil {
			log.Fatalf("Failed to connect to Lotus node '%s': %v", *nodeName, err)
		}
		defer closer()

		err = resources.InitializeWallets(ctx, api, *numWallets, tokenAmount)
		if err != nil {
			log.Fatalf("Failed to create wallets on node '%s': %v", *nodeName, err)
		}
		log.Printf("Wallets created successfully on node '%s'.", *nodeName)

	case "delete":
		log.Printf("Deleting wallets on node '%s'...", *nodeName)
		api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
		if err != nil {
			log.Fatalf("Failed to connect to Lotus node '%s': %v", *nodeName, err)
		}
		defer closer()

		allWallets, err := resources.GetAllWalletAddressesExceptGenesis(ctx, api)
		if err != nil {
			log.Fatalf("Failed to list wallets on node '%s': %v", *nodeName, err)
		}
		if len(allWallets) == 0 {
			// rand.Intn panics if allWallets == 0
			log.Printf("No wallets available to delete on node '%s'.", *nodeName)
			break
		}

		// Delete a random number of wallets
		rand.Seed(time.Now().UnixNano())
		numToDelete := rand.Intn(len(allWallets)) + 1
		walletsToDelete := allWallets[:numToDelete]

		err = resources.DeleteWallets(ctx, api, walletsToDelete)
		if err != nil {
			log.Fatalf("Failed to delete wallets on node '%s': %v", *nodeName, err)
		}
		log.Printf("Deleted %d wallets successfully on node '%s'.", numToDelete, *nodeName)

	case "spam":
		var apis []api.FullNode
		var wallets [][]address.Address

		// Gather wallets from all nodes
		for _, node := range config.Nodes {
			api, closer, err := resources.ConnectToNode(ctx, node)
			if err != nil {
				log.Fatalf("Failed to connect to Lotus node '%s': %v", node.Name, err)
			}
			defer closer()

			nodeWallets, err := resources.GetAllWalletAddressesExceptGenesis(ctx, api)
			if err != nil {
				log.Fatalf("Failed to list wallets on node '%s': %v", node.Name, err)
			}
			apis = append(apis, api)
			wallets = append(wallets, nodeWallets)
		}

		// Start spamming transactions
		rand.Seed(time.Now().UnixNano())
		numTransactions := rand.Intn(50) + 1
		log.Printf("Spamming transactions between nodes...")
		err := resources.SpamTransactions(ctx, apis, wallets, numTransactions)
		if err != nil {
			log.Fatalf("Failed to spam transactions: %v", err)
		}
		log.Println("Transaction spamming completed.")

	case "connect":
		log.Printf("Connecting node '%s' to all other nodes...", *nodeName)
		api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
		if err != nil {
			log.Fatalf("Failed to connect to Lotus node '%s': %v", *nodeName, err)
		}
		defer closer()

		var lotusNodes []resources.NodeConfig

		for _, node := range config.Nodes {
			if node.Name == "Lotus1" || node.Name == "Lotus2" {
				lotusNodes = append(lotusNodes, node)
			}
		}

		err = resources.ConnectToOtherNodes(ctx, api, *nodeConfig, lotusNodes)
		if err != nil {
			log.Fatalf("Failed to connect node '%s' to other nodes: %v", *nodeName, err)
		}
		log.Printf("Node '%s' connected successfully.", *nodeName)

	case "disconnect":
		log.Printf("Disconnecting node '%s' from all other nodes...", *nodeName)
		api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
		if err != nil {
			log.Fatalf("Failed to connect to Lotus node '%s': %v", *nodeName, err)
		}
		defer closer()

		err = resources.DisconnectFromOtherNodes(ctx, api)
		if err != nil {
			log.Fatalf("Failed to disconnect node '%s' from other nodes: %v", *nodeName, err)
		}
		log.Printf("Node '%s' disconnected successfully.", *nodeName)

	case "deploy":
		log.Printf("Deploying smart contract from file: %s on node '%s'...", *contractPath, *nodeName)

		// Connect to the selected node
		api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
		if err != nil {
			log.Fatalf("Failed to connect to Lotus node '%s': %v", *nodeName, err)
		}
		defer closer()

		// Define the funding amount
		fundingAmount, _ := new(big.Int).SetString(config.DefaultFundingAmount, 10)
		tokenAmount := abi.TokenAmount(types.BigInt{Int: fundingAmount})

		// Deploy the smart contract
		ethAddr, err := resources.DeploySmartContract(ctx, api, *contractPath, tokenAmount)
		if err != nil {
			log.Fatalf("Failed to deploy smart contract: %v", err)
		}
		log.Printf("Smart contract deployed successfully at Ethereum address: 0x%s", ethAddr.String())

	}
}
