package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
)

func parseFlags() (*string, *string, *string, *int, *string, *time.Duration, *time.Duration, *string, *time.Duration, *string, *int) {
	configFile := flag.String("config", "/opt/antithesis/resources/config.json", "Path to config JSON file")
	operation := flag.String("operation", "", "Operation: 'create', 'delete', 'spam', 'connect', 'deploySimpleCoin', 'deployMCopy', 'chaos', 'spamInvalidMessages', 'chainedInvalidTx'")
	nodeName := flag.String("node", "", "Node name from config.json (required for certain operations)")
	numWallets := flag.Int("wallets", 1, "Number of wallets for the operation (required for 'create' and 'delete')")
	contractPath := flag.String("contract", "", "Path to the smart contract bytecode file")
	minInterval := flag.Duration("min-interval", 5*time.Second, "Minimum interval between chaos operations")
	maxInterval := flag.Duration("max-interval", 30*time.Second, "Maximum interval between chaos operations")
	targetAddr := flag.String("target", "", "Target multiaddr for chaos operations")
	duration := flag.Duration("duration", 60*time.Second, "Duration to run the attack (default: 60s)")
	targetAddr2 := flag.String("target2", "", "Second target multiaddr for chain sync operations")
	count := flag.Int("count", 100, "Number of transactions/operations to perform")

	flag.Parse()
	return configFile, operation, nodeName, numWallets, contractPath, minInterval, maxInterval, targetAddr, duration, targetAddr2, count
}

func validateInputs(operation, nodeName, contractPath, targetAddr, targetAddr2 *string) error {
	validOps := map[string]bool{
		"create": true, "delete": true, "spam": true, "connect": true,
		"deploySimpleCoin": true, "deployMCopy": true,
		"deployTStore": true, "spamInvalidMessages": true, "chaos": true,
		"chainedInvalidTx": true,
	}

	// Operations that don't require a node name
	noNodeNameRequired := map[string]bool{
		"spam": true, "chaos": true,
	}

	if !validOps[*operation] {
		return fmt.Errorf("invalid operation: %s", *operation)
	}

	if !noNodeNameRequired[*operation] && *nodeName == "" {
		return fmt.Errorf("node name is required for the '%s' operation", *operation)
	}

	if *operation == "chaos" && *targetAddr == "" {
		return fmt.Errorf("target multiaddr is required for '%s' operations", *operation)
	}

	if (*operation == "deploySimpleCoin" || *operation == "deployMCopy" || *operation == "deployTStore") && *contractPath == "" {
		return fmt.Errorf("contract path is required for the '%s' operation", *operation)
	}

	return nil
}

func loadConfig(configFile string) (*resources.Config, error) {
	config, err := resources.LoadConfig(configFile)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("[INFO] Starting workload...")

	// Parse command line flags
	configFile, operation, nodeName, numWallets, contractPath, minInterval, maxInterval, targetAddr, duration, targetAddr2, count := parseFlags()

	// Create context
	ctx := context.Background()

	// Validate inputs based on operation
	if err := validateInputs(operation, nodeName, contractPath, targetAddr, targetAddr2); err != nil {
		log.Printf("[ERROR] Input validation failed: %v", err)
		os.Exit(1)
	}

	// Load configuration
	config, err := loadConfig(*configFile)
	if err != nil {
		log.Printf("[ERROR] Failed to load config: %v", err)
		os.Exit(1)
	}

	// Get node config if needed
	var nodeConfig *resources.NodeConfig
	if *nodeName != "" {
		for i := range config.Nodes {
			if config.Nodes[i].Name == *nodeName {
				nodeConfig = &config.Nodes[i]
				break
			}
		}
		if nodeConfig == nil && *operation != "spam" && *operation != "chaos" {
			log.Printf("[ERROR] Node '%s' not found in config", *nodeName)
			os.Exit(1)
		}
	}

	// Execute the requested operation
	switch *operation {
	case "create":
		err = performCreateOperation(ctx, nodeConfig, numWallets, abi.NewTokenAmount(10000))
	case "delete":
		err = performDeleteOperation(ctx, nodeConfig)
	case "spam":
		err = performSpamOperation(ctx, config)
	case "connect":
		err = performConnectDisconnectOperation(ctx, nodeConfig, config)
	case "deploySimpleCoin":
		err = performDeploySimpleCoin(ctx, nodeConfig, *contractPath)
	case "deployMCopy":
		err = performDeployMCopy(ctx, nodeConfig, *contractPath)
	case "deployTStore":
		err = performDeployTStore(ctx, nodeConfig, *contractPath)
	case "spamInvalidMessages":
		err = spamInvalidMessages(ctx, nodeConfig, *count)
	case "chainedInvalidTx":
		err = performChainedInvalidTransactions(ctx, nodeConfig, *count)
	case "chaos":
		err = performChaosOperations(ctx, *targetAddr, *minInterval, *maxInterval, *duration)
	default:
		log.Printf("[ERROR] Unknown operation: %s", *operation)
		os.Exit(1)
	}

	if err != nil {
		log.Printf("[ERROR] Operation '%s' failed: %v", *operation, err)
		os.Exit(1)
	}

	log.Printf("[INFO] Operation '%s' completed successfully", *operation)
}

func performCreateOperation(ctx context.Context, nodeConfig *resources.NodeConfig, numWallets *int, tokenAmount abi.TokenAmount) error {
	log.Printf("Creating %d wallets on node '%s'...", *numWallets, nodeConfig.Name)
	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to Lotus node '%s': %w", nodeConfig.Name, err)
	}
	defer closer()

	if err := resources.InitializeWallets(ctx, api, *numWallets, tokenAmount); err != nil {
		return fmt.Errorf("failed to create wallets on node '%s': %w", nodeConfig.Name, err)
	}

	log.Printf("Wallets created successfully on node '%s'", nodeConfig.Name)
	return nil
}

func performDeleteOperation(ctx context.Context, nodeConfig *resources.NodeConfig) error {
	log.Printf("Deleting wallets on node '%s'...", nodeConfig.Name)
	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to Lotus node '%s': %w", nodeConfig.Name, err)
	}
	defer closer()

	allWallets, err := resources.GetAllWalletAddressesExceptGenesis(ctx, api)
	if err != nil {
		return fmt.Errorf("failed to list wallets on node '%s': %w", nodeConfig.Name, err)
	}

	if len(allWallets) == 0 {
		log.Printf("No wallets available to delete on node '%s'", nodeConfig.Name)
		return nil
	}

	// Delete a random number of wallets
	rand.Seed(time.Now().UnixNano())
	numToDelete := rand.Intn(len(allWallets)) + 1
	walletsToDelete := allWallets[:numToDelete]

	if err := resources.DeleteWallets(ctx, api, walletsToDelete); err != nil {
		return fmt.Errorf("failed to delete wallets on node '%s': %w", nodeConfig.Name, err)
	}

	log.Printf("Deleted %d wallets successfully on node '%s'", numToDelete, nodeConfig.Name)
	return nil
}

func performSpamOperation(ctx context.Context, config *resources.Config) error {
	log.Println("[INFO] Starting spam operation...")
	var apis []api.FullNode
	var wallets [][]address.Address
	var closers []func()
	defer func() {
		for _, closer := range closers {
			closer()
		}
	}()

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
			return fmt.Errorf("failed to connect to Lotus node '%s': %w", node.Name, err)
		}
		closers = append(closers, closer)

		log.Printf("[INFO] Retrieving wallets for node '%s'...", node.Name)
		nodeWallets, err := resources.GetAllWalletAddressesExceptGenesis(ctx, api)
		if err != nil {
			return fmt.Errorf("failed to retrieve wallets for node '%s': %w", node.Name, err)
		}
		log.Printf("[INFO] Retrieved %d wallets for node '%s'.", len(nodeWallets), node.Name)

		apis = append(apis, api)
		wallets = append(wallets, nodeWallets)
	}

	// Ensure we have enough nodes connected for spam
	if len(apis) < 1 {
		return fmt.Errorf("not enough nodes available for spam operation")
	}

	// Perform spam transactions
	rand.Seed(time.Now().UnixNano())
	numTransactions := rand.Intn(30) + 1
	log.Printf("[INFO] Initiating spam operation with %d transactions...", numTransactions)
	if err := resources.SpamTransactions(ctx, apis, wallets, numTransactions); err != nil {
		return fmt.Errorf("spam operation failed: %w", err)
	}
	log.Println("[INFO] Spam operation completed successfully.")
	return nil
}

func performConnectDisconnectOperation(ctx context.Context, nodeConfig *resources.NodeConfig, config *resources.Config) error {
	log.Printf("Toggling connection for node '%s'...", nodeConfig.Name)
	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to Lotus node '%s': %w", nodeConfig.Name, err)
	}
	defer closer()

	var lotusNodes []resources.NodeConfig
	for _, node := range config.Nodes {
		if node.Name == "Lotus1" || node.Name == "Lotus2" {
			lotusNodes = append(lotusNodes, node)
		}
	}

	// Check current connections
	peers, err := api.NetPeers(ctx)
	if err != nil {
		return fmt.Errorf("failed to get peer list: %w", err)
	}

	// If we have peers, disconnect; otherwise connect
	if len(peers) > 0 {
		log.Printf("Node '%s' has %d peers, disconnecting...", nodeConfig.Name, len(peers))
		if err := resources.DisconnectFromOtherNodes(ctx, api); err != nil {
			return fmt.Errorf("failed to disconnect node '%s' from other nodes: %w", nodeConfig.Name, err)
		}
		log.Printf("Node '%s' disconnected successfully", nodeConfig.Name)
	} else {
		log.Printf("Node '%s' has no peers, connecting...", nodeConfig.Name)
		if err := resources.ConnectToOtherNodes(ctx, api, *nodeConfig, lotusNodes); err != nil {
			return fmt.Errorf("failed to connect node '%s' to other nodes: %w", nodeConfig.Name, err)
		}
		log.Printf("Node '%s' connected successfully", nodeConfig.Name)
	}
	return nil
}

func performDeploySimpleCoin(ctx context.Context, nodeConfig *resources.NodeConfig, contractPath string) error {
	assert.Always(nodeConfig != nil, "NodeConfig cannot be nil", nil)
	assert.Always(contractPath != "", "Contract path cannot be empty", nil)

	// Connect to Lotus node
	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	assert.Always(err == nil, "Failed to connect to Lotus node", map[string]interface{}{
		"node": nodeConfig.Name, "err": err,
	})
	defer closer()

	// Deploy the contract
	fromAddr, contractAddr := resources.DeployContractFromFilename(ctx, api, contractPath)
	assert.Always(fromAddr.String() != "", "Deployment failed: from address is empty", nil)
	assert.Always(contractAddr.String() != "", "Deployment failed: contract address is empty", nil)

	// Generate input data for owner's address
	inputData := resources.InputDataFromFrom(ctx, api, fromAddr)
	assert.Always(len(inputData) > 0, "Input data for owner's address cannot be empty", nil)

	// Invoke contract for owner's balance
	result, _, err := resources.InvokeContractByFuncName(ctx, api, fromAddr, contractAddr, "getBalance(address)", inputData)
	assert.Sometimes(err == nil, "Failed to retrieve owner's balance", map[string]interface{}{
		"fromAddr":     fromAddr,
		"contractAddr": contractAddr,
		"function":     "getBalance(address)",
		"err":          err,
	})
	expectedOwnerBalance := "0000000000000000000000000000000000000000000000000000000000002710" // Example balance in string format
	assert.Sometimes(hex.EncodeToString(result) == expectedOwnerBalance, "Owner's balance mismatch", map[string]interface{}{
		"expected": expectedOwnerBalance, "actual": hex.EncodeToString(result),
	})

	inputData[31]++
	resultt, _, err := resources.InvokeContractByFuncName(ctx, api, fromAddr, contractAddr, "getBalance(address)", inputData)
	assert.Sometimes(err == nil, "Failed to retrieve non-owner's balance", map[string]interface{}{
		"fromAddr":     fromAddr,
		"contractAddr": contractAddr,
		"function":     "getBalance(address)",
		"err":          err,
	})
	expectedNonOwnerBalance := "0000000000000000000000000000000000000000000000000000000000000000" // Example balance in string format
	assert.Sometimes(hex.EncodeToString(resultt) == expectedNonOwnerBalance, "Non-owner's balance mismatch", map[string]interface{}{
		"expected": expectedNonOwnerBalance, "actual": hex.EncodeToString(resultt),
	})

	return nil
}

func performDeployMCopy(ctx context.Context, nodeConfig *resources.NodeConfig, contractPath string) error {
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
	return nil
}

func performDeployTStore(ctx context.Context, nodeConfig *resources.NodeConfig, contractPath string) error {
	log.Printf("Deploying and invoking TStore contract on node '%s'...", nodeConfig.Name)

	// Connect to Lotus node
	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Fatalf("Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
	}
	defer closer()

	// Deploy the contract
	fromAddr, contractAddr := resources.DeployContractFromFilename(ctx, api, contractPath)

	inputData := make([]byte, 0)

	// Run initial tests
	_, _, err = resources.InvokeContractByFuncName(ctx, api, fromAddr, contractAddr, "runTests()", inputData)
	assert.Sometimes(err == nil, "Failed to invoke runTests()", map[string]interface{}{"err": err})
	fmt.Printf("InvokeContractByFuncName Error: %s", err)
	// Validate lifecycle in subsequent transactions
	_, _, err = resources.InvokeContractByFuncName(ctx, api, fromAddr, contractAddr, "testLifecycleValidationSubsequentTransaction()", inputData)
	assert.Sometimes(err == nil, "Failed to invoke testLifecycleValidationSubsequentTransaction()", map[string]interface{}{"err": err})
	fmt.Printf("InvokeContractByFuncName Error: %s", err)
	// Deploy a second contract instance for further testing
	fromAddr, contractAddr2 := resources.DeployContractFromFilename(ctx, api, contractPath)
	inputDataContract := resources.InputDataFromFrom(ctx, api, contractAddr2)
	fmt.Printf("InvokeContractByFuncName Error: %s", err)
	// Test re-entry scenarios
	_, _, err = resources.InvokeContractByFuncName(ctx, api, fromAddr, contractAddr, "testReentry(address)", inputDataContract)
	assert.Sometimes(err == nil, "Failed to invoke testReentry(address)", map[string]interface{}{"err": err})
	fmt.Printf("InvokeContractByFuncName Error: %s", err)

	// Test nested contract interactions
	_, _, err = resources.InvokeContractByFuncName(ctx, api, fromAddr, contractAddr, "testNestedContracts(address)", inputDataContract)
	assert.Sometimes(err == nil, "Failed to invoke testNestedContracts(address)", map[string]interface{}{"err": err})
	fmt.Printf("InvokeContractByFuncName Error: %s", err)

	log.Printf("TStore contract successfully deployed and tested on node '%s'.", nodeConfig.Name)
	return nil
}

func spamInvalidMessages(ctx context.Context, nodeConfig *resources.NodeConfig, count int) error {
	if nodeConfig == nil {
		return fmt.Errorf("node configuration is required for spamInvalidMessages operation")
	}

	log.Printf("Spamming %d bad transactions on node '%s'...", count, nodeConfig.Name)

	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return nil
	}
	defer closer()

	wallets, err := resources.GetAllWalletAddressesExceptGenesis(ctx, api)
	if err != nil {
		log.Printf("Failed to get wallet addresses: %v", err)
		return nil
	}

	if len(wallets) < 2 {
		log.Printf("[WARN] Not enough wallets (found %d). Creating more wallets.", len(wallets))
		numWallets := 2
		if err := performCreateOperation(ctx, nodeConfig, &numWallets, abi.NewTokenAmount(10000)); err != nil {
			log.Printf("Create operation failed: %v", err)
		}

		// Try again to get wallets
		wallets, err = resources.GetAllWalletAddressesExceptGenesis(ctx, api)
		if err != nil || len(wallets) < 2 {
			return fmt.Errorf("failed to get enough wallet addresses after creation attempt: %v", err)
		}
	}

	log.Printf("Using wallets %s and %s for spam operations", wallets[0], wallets[1])
	err = resources.SendInvalidTransactions(ctx, api, wallets[0], wallets[1], count)
	assert.Sometimes(err == nil, "Invalid transaction should sometimes pass", map[string]interface{}{"error": err})

	log.Printf("Spammed %d bad transactions on node '%s'.", count, nodeConfig.Name)
	return nil
}

func performChainedInvalidTransactions(ctx context.Context, nodeConfig *resources.NodeConfig, count int) error {
	if nodeConfig == nil {
		return fmt.Errorf("node configuration is required for chainedInvalidTx operation")
	}

	log.Printf("Starting chained invalid transaction test with %d transactions on node '%s'...", count, nodeConfig.Name)

	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return nil
	}
	defer closer()

	wallets, err := resources.GetAllWalletAddressesExceptGenesis(ctx, api)
	if err != nil {
		log.Printf("Failed to get wallet addresses: %v", err)
		return nil
	}

	if len(wallets) < 1 {
		log.Printf("[WARN] No wallets found. Creating more wallets.")
		numWallets := 2
		if err := performCreateOperation(ctx, nodeConfig, &numWallets, abi.NewTokenAmount(10000)); err != nil {
			log.Printf("Create operation failed: %v", err)
		}

		// Try again to get wallets
		wallets, err = resources.GetAllWalletAddressesExceptGenesis(ctx, api)
		if err != nil || len(wallets) < 1 {
			return fmt.Errorf("failed to get wallet addresses after creation attempt: %v", err)
		}
	}

	// Use a second wallet as target if available, otherwise use the same wallet
	var targetWallet address.Address
	if len(wallets) > 1 {
		targetWallet = wallets[1]
	} else {
		targetWallet = wallets[0]
	}

	log.Printf("Using wallet %s for transactions", wallets[0])
	if err := resources.SendInvalidTransactions(ctx, api, wallets[0], targetWallet, count); err != nil {
		return fmt.Errorf("invalid transaction test failed: %w", err)
	}

	log.Printf("Completed invalid transaction test on node '%s'", nodeConfig.Name)
	return nil
}

func performChaosOperations(ctx context.Context, targetAddr string, minInterval, maxInterval, duration time.Duration) error {
	log.Printf("Starting network chaos operations targeting %s...", targetAddr)

	chaos, err := resources.NewNetworkChaos(ctx, targetAddr)
	if err != nil {
		return fmt.Errorf("failed to initialize network chaos: %w", err)
	}

	chaos.Start(minInterval, maxInterval)

	log.Printf("Network chaos operations will run for %s...", duration)
	time.Sleep(duration)
	log.Println("Stopping network chaos operations...")
	chaos.Stop()

	return nil
}
