package main

import (
	"bytes"
	"context"
	"encoding/hex"
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

func parseFlags() (*string, *string, *string, *int, *string) {
	configFile := flag.String("config", "/opt/antithesis/resources/config.json", "Path to config JSON file")
	operation := flag.String("operation", "", "Operation: 'create', 'delete', 'spam', 'connect', 'disconnect', 'deploySimpleCoin', or 'deployMCopy'")
	nodeName := flag.String("node", "", "Node name from config.json (required for certain operations)")
	numWallets := flag.Int("wallets", 1, "Number of wallets for the operation (required for 'create' and 'delete')")
	contractPath := flag.String("contract", "", "Path to the smart contract bytecode file")

	flag.Parse()
	return configFile, operation, nodeName, numWallets, contractPath
}

func validateInputs(operation, nodeName, contractPath *string) {
	if *operation != "create" && *operation != "delete" && *operation != "spam" && *operation != "connect" && *operation != "disconnect" && *operation != "deploySimpleCoin" && *operation != "deployMCopy" {
		log.Fatalf("Invalid operation: %s. Use 'create', 'delete', 'spam', 'connect', 'disconnect', 'deploySimpleCoin', or 'deployMCopy'.", *operation)
	}
	if (*operation == "create" || *operation == "delete" || *operation == "connect" || *operation == "disconnect" || *operation == "deploySimpleCoin" || *operation == "deployMCopy") && *nodeName == "" {
		log.Fatalf("Node name is required for the '%s' operation.", *operation)
	}
	if (*operation == "deploySimpleCoin" || *operation == "deployMCopy") && *contractPath == "" {
		log.Fatalf("Contract path is required for the '%s' operation.", *operation)
	}
}

func loadConfig(configFile string) (*resources.Config, error) {
	config, err := resources.LoadConfig(configFile)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func main() {
	ctx := context.Background()

	// Parse CLI flags
	configFile, operation, nodeName, numWallets, contractPath := parseFlags()

	// Validate inputs
	validateInputs(operation, nodeName, contractPath)

	// Load configuration
	config, err := loadConfig(*configFile)
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
	if (*operation == "create" || *operation == "delete" || *operation == "connect" || *operation == "disconnect" || *operation == "deploySimpleCoin" || *operation == "deployMCopy") && nodeConfig == nil {
		log.Fatalf("Node '%s' not found in config.json.", *nodeName)
	}

	// Perform operations
	fundingAmount, _ := new(big.Int).SetString(config.DefaultFundingAmount, 10)
	tokenAmount := abi.TokenAmount(types.BigInt{Int: fundingAmount})

	switch *operation {
	case "create":
		performCreateOperation(ctx, nodeConfig, numWallets, tokenAmount)
	case "delete":
		performDeleteOperation(ctx, nodeConfig)
	case "spam":
		performSpamOperation(ctx, config)
	case "connect":
		performConnectOperation(ctx, nodeConfig, config)
	case "disconnect":
		performDisconnectOperation(ctx, nodeConfig)
	case "deploySimpleCoin":
		performDeploySimpleCoin(ctx, nodeConfig, *contractPath)
	case "deployMCopy":
		performDeployMCopy(ctx, nodeConfig, *contractPath)
	}
}

func performCreateOperation(ctx context.Context, nodeConfig *resources.NodeConfig, numWallets *int, tokenAmount abi.TokenAmount) {
	log.Printf("Creating %d wallets on node '%s'...", *numWallets, nodeConfig.Name)
	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Fatalf("Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
	}
	defer closer()

	err = resources.InitializeWallets(ctx, api, *numWallets, tokenAmount)
	if err != nil {
		log.Fatalf("Failed to create wallets on node '%s': %v", nodeConfig.Name, err)
	}
	log.Printf("Wallets created successfully on node '%s'.", nodeConfig.Name)
}

func performDeleteOperation(ctx context.Context, nodeConfig *resources.NodeConfig) {
	log.Printf("Deleting wallets on node '%s'...", nodeConfig.Name)
	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Fatalf("Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
	}
	defer closer()

	allWallets, err := resources.GetAllWalletAddressesExceptGenesis(ctx, api)
	if err != nil {
		log.Fatalf("Failed to list wallets on node '%s': %v", nodeConfig.Name, err)
	}
	if len(allWallets) == 0 {
		log.Printf("No wallets available to delete on node '%s'.", nodeConfig.Name)
		return
	}

	rand.Seed(time.Now().UnixNano())
	numToDelete := rand.Intn(len(allWallets)) + 1
	walletsToDelete := allWallets[:numToDelete]

	err = resources.DeleteWallets(ctx, api, walletsToDelete)
	if err != nil {
		log.Fatalf("Failed to delete wallets on node '%s': %v", nodeConfig.Name, err)
	}
	log.Printf("Deleted %d wallets successfully on node '%s'.", numToDelete, nodeConfig.Name)
}

func performSpamOperation(ctx context.Context, config *resources.Config) {
	log.Println("[INFO] Starting spam operation...")
	var apis []api.FullNode
	var wallets [][]address.Address

	// Filter nodes for operation
	filteredNodes := []resources.NodeConfig{}
	for _, node := range config.Nodes {
		if node.Name == "Lotus1" || node.Name == "Lotus2" {
			filteredNodes = append(filteredNodes, node)
		}
	}
	log.Printf("[INFO] Filtered nodes for spam operation: %+v", filteredNodes)

	// Connect to each node and retrieve wallets
	for _, node := range filteredNodes {
		log.Printf("[INFO] Connecting to Lotus node '%s'...", node.Name)
		api, closer, err := resources.ConnectToNode(ctx, node)
		if err != nil {
			log.Fatalf("[ERROR] Failed to connect to Lotus node '%s': %v", node.Name, err)
		}
		defer closer()

		log.Printf("[INFO] Retrieving wallets for node '%s'...", node.Name)
		nodeWallets, err := resources.GetAllWalletAddressesExceptGenesis(ctx, api)
		if err != nil {
			log.Fatalf("[ERROR] Failed to retrieve wallets for node '%s': %v", node.Name, err)
		}
		log.Printf("[INFO] Retrieved %d wallets for node '%s'.", len(nodeWallets), node.Name)

		apis = append(apis, api)
		wallets = append(wallets, nodeWallets)
	}

	// Perform spam transactions
	rand.Seed(time.Now().UnixNano())
	numTransactions := rand.Intn(30) + 1
	log.Printf("[INFO] Initiating spam operation with %d transactions...", numTransactions)
	err := resources.SpamTransactions(ctx, apis, wallets, numTransactions)
	if err != nil {
		log.Fatalf("[ERROR] Spam operation failed: %v", err)
	}
	log.Println("[INFO] Spam operation completed successfully.")
}

func performConnectOperation(ctx context.Context, nodeConfig *resources.NodeConfig, config *resources.Config) {
	log.Printf("Connecting node '%s' to all other nodes...", nodeConfig.Name)
	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Fatalf("Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
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
		log.Fatalf("Failed to connect node '%s' to other nodes: %v", nodeConfig.Name, err)
	}
	log.Printf("Node '%s' connected successfully.", nodeConfig.Name)
}

func performDisconnectOperation(ctx context.Context, nodeConfig *resources.NodeConfig) {
	log.Printf("Disconnecting node '%s' from all other nodes...", nodeConfig.Name)
	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Fatalf("Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
	}
	defer closer()

	err = resources.DisconnectFromOtherNodes(ctx, api)
	if err != nil {
		log.Fatalf("Failed to disconnect node '%s' from other nodes: %v", nodeConfig.Name, err)
	}
	log.Printf("Node '%s' disconnected successfully.", nodeConfig.Name)
}

func performDeploySimpleCoin(ctx context.Context, nodeConfig *resources.NodeConfig, contractPath string) {
	log.Printf("[INFO] Starting deployment and invocation of SimpleCoin contract on node '%s'.", nodeConfig.Name)

	// Connect to Lotus node
	log.Printf("[INFO] Connecting to Lotus node '%s'.", nodeConfig.Name)
	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Fatalf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
	}
	defer closer()
	log.Printf("[INFO] Successfully connected to Lotus node '%s'.", nodeConfig.Name)

	// Deploy the contract
	log.Printf("[INFO] Deploying SimpleCoin contract from path: %s", contractPath)
	fromAddr, contractAddr := resources.DeployContractFromFilename(ctx, api, contractPath)
	log.Printf("[INFO] Contract deployed successfully. From Address: %s, Contract Address: %s", fromAddr, contractAddr)
	{
		// Generate input data for owner's address
		log.Printf("[INFO] Generating input data for owner's address: %s", fromAddr)
		inputData := resources.InputDataFromFrom(ctx, api, fromAddr)
		log.Printf("[INFO] Input data for owner generated: %x", inputData)

		// Invoke contract for owner's balance
		log.Printf("[INFO] Invoking 'getBalance(address)' on contract with owner's address.")
		result, _, err := resources.InvokeContractByFuncName(ctx, api, fromAddr, contractAddr, "getBalance(address)", inputData)
		if err != nil {
			log.Fatalf("[ERROR] Failed to invoke SimpleCoin contract with owner's address: %v", err)
		}
		log.Printf("[INFO] Owner's balance retrieved successfully: %x", result)
	}
	{
		inputData := resources.InputDataFromFrom(ctx, api, fromAddr)
		// Modify input data for non-owner address
		log.Printf("[INFO] Modifying input data for non-owner address.")
		inputData[31]++
		log.Printf("[INFO] Modified input data for non-owner: %x", inputData)

		// Invoke contract for non-owner's balance
		log.Printf("[INFO] Invoking 'getBalance(address)' on contract with non-owner's address.")
		result, _, err := resources.InvokeContractByFuncName(ctx, api, fromAddr, contractAddr, "getBalance(address)", inputData)
		if err != nil {
			log.Fatalf("[ERROR] Failed to invoke SimpleCoin contract with non-owner's address: %v", err)
		}
		log.Printf("[INFO] Non-owner's balance retrieved successfully: %x", result)
	}
	log.Printf("[INFO] Completed deployment and invocation of SimpleCoin contract on node '%s'.", nodeConfig.Name)
}

func performDeployMCopy(ctx context.Context, nodeConfig *resources.NodeConfig, contractPath string) {
	log.Printf("Deploying and invoking MCopy contract on node '%s'...", nodeConfig.Name)
	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Fatalf("Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
	}
	defer closer()

	fromAddr, contractAddr := resources.DeployContractFromFilename(ctx, api, contractPath)

	hexString := "000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000087465737464617461000000000000000000000000000000000000000000000000"
	inputArgument, err := hex.DecodeString(hexString)
	if err != nil {
		log.Fatalf("Failed to decode input argument: %v", err)
	}

	result, _, err := resources.InvokeContractByFuncName(ctx, api, fromAddr, contractAddr, "optimizedCopy(bytes)", inputArgument)
	if err != nil {
		log.Fatalf("Failed to invoke MCopy contract: %v", err)
	}
	if bytes.Compare(result, inputArgument) == 0 {
		log.Printf("MCopy invocation result matches the input argument. No change in the output.")
	} else {
		log.Printf("MCopy invocation result: %x\n", result)
	}
}
