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
	mpoolfuzz "github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources/mpool-fuzz"
	p2pfuzz "github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources/p2p-fuzz"
	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

// HOW TO ADD A NEW CLI COMMAND TO THIS TOOL
// Follow these steps to add a new command-line operation:
//
// STEP 1: Add a new flag in the parseFlags() function
//   - Define your flag using flag.* (e.g., flag.Bool, flag.String, etc.)
//   - Update the operation list in the -operation flag description
//   - Update the function signature and return statement if your flag needs to be returned
//
// STEP 2: Update the validateInputs() function
//   - Add your operation to the validOps map (e.g., "myNewOperation": true)
//   - If your operation doesn't require a node name, add it to noNodeNameRequired
//   - Add any custom validation logic specific to your operation
//
// STEP 3: Add a case in the switch statement in main()
//   - Add a case for your operation (e.g., case "myNewOperation":)
//   - Call your operation's implementation function (e.g., err = performMyNewOperation(...))
//
// STEP 4: Implement your operation's function
//   - Create a new function (e.g., func performMyNewOperation(ctx context.Context, ...) error {})
//   - Add your operation's logic inside the function
//   - Make sure to return nil for success or an error if something fails
//   - Follow the pattern of other operation functions for consistency

func parseFlags() (*string, *string, *string, *int, *string, *time.Duration, *time.Duration, *string, *time.Duration, *string, *int, *string, *int) {
	// STEP 1: Define a new flag for your operation here.
	// For example, if your new operation is "myNewOperation":
	// myNewOperationFlag := flag.Bool("myNewOperation", false, "Description of myNewOperation")
	// Remember to update the function signature and return statement if you add new flags that need to be returned.
	configFile := flag.String("config", "/opt/antithesis/resources/config.json", "Path to config JSON file")
	operation := flag.String("operation", "", "Operation: 'create', 'delete', 'spam', 'connect', 'deploySimpleCoin', 'deployMCopy', 'chaos', 'mempoolFuzz', 'pingAttack'")
	nodeName := flag.String("node", "", "Node name from config.json (required for certain operations)")
	numWallets := flag.Int("wallets", 1, "Number of wallets for the operation (required for 'create' and 'delete')")
	contractPath := flag.String("contract", "", "Path to the smart contract bytecode file")
	minInterval := flag.Duration("min-interval", 5*time.Second, "Minimum interval between operations")
	maxInterval := flag.Duration("max-interval", 30*time.Second, "Maximum interval between operations")
	targetAddr := flag.String("target", "", "Target multiaddr for chaos operations")
	duration := flag.Duration("duration", 60*time.Second, "Duration to run the attack (default: 60s)")
	targetAddr2 := flag.String("target2", "", "Second target multiaddr for chain sync operations")
	count := flag.Int("count", 100, "Number of transactions/operations to perform")
	pingAttackType := flag.String("ping-attack-type", "random", "Type of ping attack: random, oversized, empty, multiple, incomplete")
	concurrency := flag.Int("concurrency", 5, "Number of concurrent operations for attacks")
	flag.Parse()
	// STEP 1 (continued): If you added a new flag that needs to be returned, add it to the return statement.
	// For example, if you added myNewOperationFlag:
	// return configFile, operation, nodeName, numWallets, contractPath, minInterval, maxInterval, targetAddr, duration, targetAddr2, count, pingAttackType, concurrency, myNewOperationFlag
	return configFile, operation, nodeName, numWallets, contractPath, minInterval, maxInterval, targetAddr, duration, targetAddr2, count, pingAttackType, concurrency
}

func validateInputs(operation, nodeName, contractPath, targetAddr, targetAddr2, pingAttackType *string) error {
	// STEP 2: Add your new operation to the validOps map.
	// For example:
	// "myNewOperation": true,
	validOps := map[string]bool{
		"create":           true,
		"delete":           true,
		"spam":             true,
		"connect":          true,
		"deploySimpleCoin": true,
		"deployMCopy":      true,
		"deployTStore":     true,
		"chaos":            true,
		"mempoolFuzz":      true,
		"pingAttack":       true,
	}

	// Operations that don't require a node name
	// STEP 2 (continued): If your new operation does not require a node name, add it here.
	// For example:
	// "myNewOperation": true,
	noNodeNameRequired := map[string]bool{
		"spam":       true,
		"chaos":      true,
		"pingAttack": true,
	}

	if !validOps[*operation] {
		return fmt.Errorf("invalid operation: %s", *operation)
	}

	if !noNodeNameRequired[*operation] && *nodeName == "" {
		return fmt.Errorf("node name is required for the '%s' operation", *operation)
	}

	if (*operation == "chaos" || *operation == "pingAttack") && *targetAddr == "" {
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
	configFile, operation, nodeName, numWallets, contractPath, minInterval, maxInterval, targetAddr, duration, targetAddr2, count, pingAttackType, concurrency := parseFlags()

	// Create context
	ctx := context.Background()

	// Validate inputs based on operation
	if err := validateInputs(operation, nodeName, contractPath, targetAddr, targetAddr2, pingAttackType); err != nil {
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
	// STEP 3: Add a case for your new operation.
	// This will call the function that implements your operation's logic.
	// For example:
	// case "myNewOperation":
	//    err = performMyNewOperation(ctx, nodeConfig /*, other_flags... */)
	case "create":
		err = performCreateOperation(ctx, nodeConfig, numWallets, abi.NewTokenAmount(1000000000000000))
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
	case "chaos":
		err = performChaosOperations(ctx, *targetAddr, *minInterval, *maxInterval, *duration)
	case "mempoolFuzz":
		err = performMempoolFuzz(ctx, nodeConfig, *count, *concurrency)
	case "pingAttack":
		attackType := p2pfuzz.AttackTypeFromString(*pingAttackType)
		err = performPingAttack(ctx, *targetAddr, attackType, *concurrency, *minInterval, *maxInterval, *duration)
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

// STEP 4: Define a new function for your operation.
// This function will contain the logic for your new CLI command.
// For example:
/*
func performMyNewOperation(ctx context.Context, nodeConfig *resources.NodeConfig) error {
	log.Println("[INFO] Starting myNewOperation...")
	// Add your operation's logic here
	log.Println("[INFO] myNewOperation completed.")
	return nil
}
*/

func performCreateOperation(ctx context.Context, nodeConfig *resources.NodeConfig, numWallets *int, tokenAmount abi.TokenAmount) error {
	log.Printf("Creating %d wallets on node '%s'...", *numWallets, nodeConfig.Name)

	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return nil
	}
	defer closer()

	err = resources.InitializeWallets(ctx, api, *numWallets, tokenAmount)
	if err != nil {
		log.Printf("Warning: Error occurred during wallet initialization: %v", err)
	} else {
		log.Printf("Wallets created successfully on node '%s'", nodeConfig.Name)
	}
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

		// Ensure wallets have sufficient funds before proceeding
		log.Printf("[INFO] Checking wallet funds for node '%s'...", node.Name)
		_, err = resources.GetAllWalletAddressesExceptGenesis(ctx, api)
		if err != nil {
			log.Printf("[WARN] Failed to ensure wallets are funded on '%s': %v", node.Name, err)
			// Create some wallets if needed
			numWallets := 3
			log.Printf("[INFO] Creating %d new wallets on node '%s'...", numWallets, node.Name)
			if err := resources.InitializeWallets(ctx, api, numWallets, abi.NewTokenAmount(1000000000000000)); err != nil {
				log.Printf("[WARN] Failed to create new wallets: %v", err)
			}
		}

		log.Printf("[INFO] Retrieving wallets for node '%s'...", node.Name)
		nodeWallets, err := resources.GetAllWalletAddressesExceptGenesis(ctx, api)
		if err != nil {
			return fmt.Errorf("failed to retrieve wallets for node '%s': %w", node.Name, err)
		}
		log.Printf("[INFO] Retrieved %d wallets for node '%s'.", len(nodeWallets), node.Name)

		if len(nodeWallets) < 2 {
			log.Printf("[WARN] Not enough wallets on node '%s' (found %d). At least 2 needed for spam operation.",
				node.Name, len(nodeWallets))
			continue
		}

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

	log.Printf("[INFO] Deploying SimpleCoin contract on node %s from %s", nodeConfig.Name, contractPath)

	// Connect to Lotus node
	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return fmt.Errorf("failed to connect to Lotus node: %w", err)
	}
	defer closer()

	// Verify contract file exists
	if _, err := os.Stat(contractPath); os.IsNotExist(err) {
		log.Printf("[ERROR] Contract file not found: %s", contractPath)
		return fmt.Errorf("contract file not found: %s", contractPath)
	}

	// Check if we have a default wallet address
	defaultAddr, err := api.WalletDefaultAddress(ctx)
	if err != nil || defaultAddr.Empty() {
		log.Printf("[WARN] No default wallet address found, attempting to get or create one")

		// Get all available addresses
		addresses, err := api.WalletList(ctx)
		if err != nil {
			return fmt.Errorf("failed to list wallet addresses: %w", err)
		}

		// If we have addresses, set the first one as default
		if len(addresses) > 0 {
			defaultAddr = addresses[0]
			log.Printf("[INFO] Using existing wallet address: %s", defaultAddr)
			err = api.WalletSetDefault(ctx, defaultAddr)
			if err != nil {
				log.Printf("[WARN] Failed to set default wallet address: %v", err)
			}
		} else {
			// Create a new address if none exists
			log.Printf("[INFO] No wallet addresses found, creating a new one")
			newAddr, err := api.WalletNew(ctx, types.KTSecp256k1)
			if err != nil {
				return fmt.Errorf("failed to create new wallet address: %w", err)
			}
			defaultAddr = newAddr
			log.Printf("[INFO] Created new wallet address: %s", defaultAddr)

			err = api.WalletSetDefault(ctx, defaultAddr)
			if err != nil {
				log.Printf("[WARN] Failed to set default wallet address: %v", err)
			}
		}
	}

	log.Printf("[INFO] Using wallet address: %s", defaultAddr)

	// Verify the address has funds before deploying
	balance, err := api.WalletBalance(ctx, defaultAddr)
	if err != nil {
		log.Printf("[WARN] Failed to check wallet balance: %v", err)
	} else if balance.IsZero() {
		log.Printf("[WARN] Wallet has zero balance, contract deployment may fail")
	}

	// Deploy the contract
	log.Printf("[INFO] Deploying contract from %s", contractPath)
	fromAddr, contractAddr := resources.DeployContractFromFilename(ctx, api, contractPath)

	if fromAddr.Empty() {
		return fmt.Errorf("deployment failed: empty from address")
	}

	if contractAddr.Empty() {
		return fmt.Errorf("deployment failed: empty contract address")
	}

	log.Printf("[INFO] Contract deployed from %s to %s", fromAddr, contractAddr)

	// Generate input data for owner's address
	inputData := resources.InputDataFromFrom(ctx, api, fromAddr)
	if len(inputData) == 0 {
		return fmt.Errorf("failed to generate input data for owner's address")
	}

	// Invoke contract for owner's balance
	log.Printf("[INFO] Checking owner's balance")
	result, _, err := resources.InvokeContractByFuncName(ctx, api, fromAddr, contractAddr, "getBalance(address)", inputData)
	if err != nil {
		log.Printf("[WARN] Failed to retrieve owner's balance: %v", err)
	} else {
		log.Printf("[INFO] Owner's balance: %x", result)
	}

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

func performChaosOperations(ctx context.Context, targetAddr string, minInterval, maxInterval, duration time.Duration) error {
	log.Printf("Starting network chaos operations targeting %s...", targetAddr)

	chaos, err := p2pfuzz.NewNetworkChaos(ctx, targetAddr)
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

func performPingAttack(ctx context.Context, targetAddr string, attackType p2pfuzz.PingAttackType, concurrency int, minInterval, maxInterval, duration time.Duration) error {
	log.Printf("[INFO] Starting ping attack against %s with attack type: %s", targetAddr, attackType)
	pinger, err := p2pfuzz.NewMaliciousPinger(ctx, targetAddr)
	if err != nil {
		return fmt.Errorf("failed to create malicious pinger: %w", err)
	}
	pinger.Start(attackType, concurrency, minInterval, maxInterval)
	runCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	<-runCtx.Done()
	pinger.Stop()
	log.Printf("[INFO] Ping attack completed")
	return nil
}

func performMempoolFuzz(ctx context.Context, nodeConfig *resources.NodeConfig, count, concurrency int) error {
	log.Printf("[INFO] Starting mempool fuzzing on node '%s' with %d transactions...", nodeConfig.Name, count)

	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return err
	}
	defer closer()

	wallets, err := resources.GetAllWalletAddressesExceptGenesis(ctx, api)
	if err != nil {
		log.Printf("[ERROR] Failed to get wallet addresses: %v", err)
		return err
	}

	if len(wallets) < 2 {
		log.Printf("[WARN] Not enough wallets (found %d). Creating more wallets.", len(wallets))
		numWallets := 2
		if err := performCreateOperation(ctx, nodeConfig, &numWallets, abi.NewTokenAmount(1000000000000000)); err != nil {
			log.Printf("Create operation failed: %v", err)
		}

		wallets, err = resources.GetAllWalletAddressesExceptGenesis(ctx, api)
		if err != nil || len(wallets) < 2 {
			return fmt.Errorf("failed to get enough wallet addresses after creation attempt: %v", err)
		}
	}
	config := mpoolfuzz.DefaultConfig()
	config.Count = count
	config.Concurrenct = concurrency

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	strategies := []string{"standard", "chained", "burst", "subtle", "edge"}
	strategy := strategies[r.Intn(len(strategies))]

	log.Printf("[INFO] Selected fuzzing strategy: %s", strategy)

	if strategy == "standard" {
		err = mpoolfuzz.SendStandardMutations(ctx, api, wallets[0], wallets[1], count, r)
	} else if strategy == "chained" {
		err = mpoolfuzz.SendChainedTransactions(ctx, api, wallets[0], wallets[1], count, r)
	} else if strategy == "burst" {
		err = mpoolfuzz.SendConcurrentBurst(ctx, api, wallets[0], wallets[1], count, r, concurrency)
	} else if strategy == "subtle" {
		err = mpoolfuzz.SendSubtleAttacks(ctx, api, wallets[0], wallets[1], count, r)
	} else {
		err = mpoolfuzz.SendEdgeCases(ctx, api, wallets[0], wallets[1], count, r)
	}

	if err != nil {
		log.Printf("[ERROR] Mempool fuzzing failed: %v", err)
		return err
	}

	log.Printf("[INFO] Mempool fuzzing completed successfully")
	return nil
}
