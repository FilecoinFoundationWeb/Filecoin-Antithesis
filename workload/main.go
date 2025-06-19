package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
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
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
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

func parseFlags() (*string, *string, *string, *int, *string, *time.Duration, *time.Duration, *string, *time.Duration, *string, *int, *string, *int, *string, *int64) {
	configFile := flag.String("config", "/opt/antithesis/resources/config.json", "Path to config JSON file")
	operation := flag.String("operation", "", "Operation: 'create', 'delete', 'spam', 'connect', 'deploySimpleCoin', 'deployMCopy', 'chaos', 'mempoolFuzz', 'pingAttack', 'rpcBenchmark', 'eth_chainId', 'checkConsensus', 'deployContract', 'stateMismatch', 'sendConsensusFault', 'checkEthMethods', 'sendEthLegacy', 'checkSplitstore', 'incomingblock', 'blockfuzz', 'checkBackfill'")
	nodeName := flag.String("node", "", "Node name from config.json (required for certain operations)")
	numWallets := flag.Int("wallets", 1, "Number of wallets for the operation")
	contractPath := flag.String("contract", "", "Path to the smart contract bytecode file")
	minInterval := flag.Duration("min-interval", 5*time.Second, "Minimum interval between operations")
	maxInterval := flag.Duration("max-interval", 30*time.Second, "Maximum interval between operations")
	targetAddr := flag.String("target", "", "Target multiaddr for chaos operations")
	duration := flag.Duration("duration", 60*time.Second, "Duration to run the attack")
	targetAddr2 := flag.String("target2", "", "Second target multiaddr for chain sync operations")
	count := flag.Int("count", 100, "Number of transactions/operations to perform")
	pingAttackType := flag.String("ping-attack-type", "random", "Type of ping attack")
	concurrency := flag.Int("concurrency", 5, "Number of concurrent operations")
	rpcURL := flag.String("rpc-url", "", "RPC URL for eth_chainId operation")
	height := flag.Int64("height", 0, "Chain height to check consensus at (0 for current height)")

	flag.Parse()
	return configFile, operation, nodeName, numWallets, contractPath, minInterval, maxInterval, targetAddr, duration, targetAddr2, count, pingAttackType, concurrency, rpcURL, height
}

func validateInputs(operation, nodeName, contractPath, targetAddr, targetAddr2, pingAttackType *string) error {
	validOps := map[string]bool{
		"create":             true,
		"delete":             true,
		"spam":               true,
		"connect":            true,
		"deploySimpleCoin":   true,
		"deployMCopy":        true,
		"deployTStore":       true,
		"chaos":              true,
		"mempoolFuzz":        true,
		"pingAttack":         true,
		"createEthAccount":   true,
		"rpc-benchmark":      true,
		"deployValueSender":  true,
		"checkConsensus":     true,
		"deployContract":     true,
		"stateMismatch":      true,
		"sendConsensusFault": true,
		"checkEthMethods":    true,
		"sendEthLegacy":      true,
		"checkSplitstore":    true,
		"incomingblock":      true,
		"blockfuzz":          true,
		"checkBackfill":      true,
	}

	// Operations that don't require a node name
	noNodeNameRequired := map[string]bool{
		"spam":               true,
		"chaos":              true,
		"pingAttack":         true,
		"rpc-benchmark":      true,
		"checkConsensus":     true,
		"sendConsensusFault": true,
		"checkEthMethods":    true,
		"checkSplitstore":    true,
		"checkBackfill":      true,
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
	configFile, operation, nodeName, numWallets, contractPath, minInterval, maxInterval, targetAddr, duration, targetAddr2, count, pingAttackType, concurrency, rpcURL, height := parseFlags()

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
	case "rpc-benchmark":
		callV2API(*rpcURL)
	case "checkConsensus":
		err = performConsensusCheck(ctx, config, *height)
	case "deployContract":
		if *contractPath == "" {
			*contractPath = "workload/resources/smart-contracts/SimpleCoin.hex"
		}
		err = deploySmartContract(ctx, nodeConfig, *contractPath)
	case "stateMismatch":
		err = performStateMismatch(ctx, nodeConfig)
	case "sendConsensusFault":
		err = performSendConsensusFault(ctx)
	case "checkEthMethods":
		err = performEthMethodsCheck(ctx)
	case "sendEthLegacy":
		err = sendEthLegacyTransaction(ctx, nodeConfig)
	case "blockfuzz":
		err = performBlockFuzzing(ctx, nodeConfig)
	case "checkBackfill":
		err = performCheckBackfill(ctx, config)
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
		if err := performCreateOperation(ctx, nodeConfig, &numWallets, types.FromFil(100)); err != nil {
			log.Printf("Create operation failed: %v", err)
		}

		wallets, err = resources.GetAllWalletAddressesExceptGenesis(ctx, api)
		if err != nil || len(wallets) < 2 {
			return fmt.Errorf("failed to get enough wallet addresses after creation attempt: %v", err)
		}
	}

	from := wallets[0]
	errCount := 0

	// Log initial balance
	balance, err := api.WalletBalance(ctx, from)
	if err != nil {
		log.Printf("[WARN] Failed to get sender balance: %v", err)
	} else {
		log.Printf("[INFO] Sender %s initial balance: %s", from, types.FIL(balance))
	}

	for i := 0; i < count; i++ {
		to, err := mpoolfuzz.GenerateRandomAddress()
		if err != nil {
			log.Printf("[WARN] Failed to generate random address: %v", err)
			continue
		}

		msg := mpoolfuzz.CreateBaseMessage(from, to, 0)
		log.Printf("[DEBUG] Pushing message %d: From=%s To=%s Value=%s GasLimit=%d",
			i, msg.From, msg.To, types.FIL(msg.Value), msg.GasLimit)

		signedMsg, err := api.MpoolPushMessage(ctx, msg, nil)
		if err != nil {
			errCount++
			log.Printf("[WARN] Failed to push message %d: %v", i, err)
			continue
		}

		log.Printf("[INFO] Message %d sent successfully: CID=%s", i, signedMsg.Cid())

		// Get mempool pending count
		pending, err := api.MpoolPending(ctx, types.EmptyTSK)
		if err != nil {
			log.Printf("[WARN] Failed to get pending messages: %v", err)
		} else {
			log.Printf("[DEBUG] Current mempool pending count: %d", len(pending))
		}

		time.Sleep(100 * time.Millisecond) // Small delay to avoid overwhelming the node
	}

	// Log final balance
	balance, err = api.WalletBalance(ctx, from)
	if err != nil {
		log.Printf("[WARN] Failed to get sender final balance: %v", err)
	} else {
		log.Printf("[INFO] Sender %s final balance: %s", from, types.FIL(balance))
	}

	log.Printf("[INFO] Mempool fuzzing completed. %d messages sent, %d errors", count, errCount)
	return nil
}

func callV2API(endpoint string) {
	log.Printf("[INFO] Starting V2 API tests on endpoint: %s", endpoint)

	// Run standard tests
	log.Printf("[INFO] Running standard V2 API tests...")
	resources.RunV2APITests(endpoint, 5*time.Second)

	// Run load tests
	log.Printf("[INFO] Running V2 API load tests...")
	resources.RunV2APILoadTest(endpoint, 10*time.Second, 5, 10)

	log.Printf("[INFO] V2 API testing completed")
}

func performConsensusCheck(ctx context.Context, config *resources.Config, height int64) error {
	log.Printf("[INFO] Starting consensus check...")

	checker, err := resources.NewConsensusChecker(ctx, config.Nodes)
	if err != nil {
		return fmt.Errorf("failed to create consensus checker: %w", err)
	}

	// If height is 0, we'll let the checker pick a random height
	if height == 0 {
		log.Printf("[INFO] No specific height provided, will check consensus at a random height")
	} else {
		log.Printf("[INFO] Will check consensus starting at height %d", height)
	}

	// Run the consensus check
	err = checker.CheckConsensus(ctx, abi.ChainEpoch(height))
	if err != nil {
		return fmt.Errorf("consensus check failed: %w", err)
	}

	log.Printf("[INFO] Consensus check completed successfully")
	return nil
}

func deploySmartContract(ctx context.Context, nodeConfig *resources.NodeConfig, contractPath string) error {
	log.Printf("[INFO] Deploying smart contract from %s...", contractPath)

	// Connect to Lotus node
	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return fmt.Errorf("failed to connect to Lotus node: %w", err)
	}
	defer closer()

	// Create new account for deployment
	key, ethAddr, deployer := resources.NewAccount()
	log.Printf("[INFO] Created new account - deployer: %s, ethAddr: %s", deployer, ethAddr)

	// Get funds from default account
	defaultAddr, err := api.WalletDefaultAddress(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to get default wallet address: %v", err)
		return fmt.Errorf("failed to get default wallet address: %w", err)
	}

	// Send funds to deployer account
	log.Printf("[INFO] Sending funds to deployer account...")
	err = resources.SendFunds(ctx, api, defaultAddr, deployer, types.FromFil(10))
	if err != nil {
		return fmt.Errorf("failed to send funds to deployer: %w", err)
	}

	// Wait for funds to be available
	log.Printf("[INFO] Waiting for funds to be available...")
	time.Sleep(30 * time.Second)

	// Read and decode contract
	contractHex, err := os.ReadFile(contractPath)
	if err != nil {
		log.Printf("[ERROR] Failed to read contract file: %v", err)
		return fmt.Errorf("failed to read contract file: %w", err)
	}
	contract, err := hex.DecodeString(string(contractHex))
	if err != nil {
		log.Printf("[ERROR] Failed to decode contract: %v", err)
		return fmt.Errorf("failed to decode contract: %w", err)
	}

	// Estimate gas
	gasParams, err := json.Marshal(ethtypes.EthEstimateGasParams{Tx: ethtypes.EthCall{
		From: &ethAddr,
		Data: contract,
	}})
	if err != nil {
		log.Printf("[ERROR] Failed to marshal gas params: %v", err)
		return fmt.Errorf("failed to marshal gas params: %w", err)
	}
	gasLimit, err := api.EthEstimateGas(ctx, gasParams)
	if err != nil {
		log.Printf("[ERROR] Failed to estimate gas: %v", err)
		return fmt.Errorf("failed to estimate gas: %w", err)
	}

	// Get gas fees
	maxPriorityFee, err := api.EthMaxPriorityFeePerGas(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to get max priority fee: %v", err)
		return fmt.Errorf("failed to get max priority fee: %w", err)
	}

	// Get nonce
	nonce, err := api.MpoolGetNonce(ctx, deployer)
	if err != nil {
		log.Printf("[ERROR] Failed to get nonce: %v", err)
		return fmt.Errorf("failed to get nonce: %w", err)
	}

	// Create transaction
	tx := ethtypes.Eth1559TxArgs{
		ChainID:              31415926,
		Value:                big.Zero(),
		Nonce:                int(nonce),
		MaxFeePerGas:         types.NanoFil,
		MaxPriorityFeePerGas: big.Int(maxPriorityFee),
		GasLimit:             int(gasLimit),
		Input:                contract,
		V:                    big.Zero(),
		R:                    big.Zero(),
		S:                    big.Zero(),
	}

	// Sign and submit transaction
	log.Printf("[INFO] Signing and submitting transaction...")
	resources.SignTransaction(&tx, key.PrivateKey)
	txHash := resources.SubmitTransaction(ctx, api, &tx)
	log.Printf("[INFO] Transaction submitted with hash: %s", txHash)

	assert.Sometimes(txHash != ethtypes.EmptyEthHash, "Transaction must be submitted successfully", map[string]interface{}{
		"tx_hash":     txHash.String(),
		"deployer":    deployer.String(),
		"requirement": "Transaction hash must not be empty",
	})

	// Wait for transaction to be mined
	log.Printf("[INFO] Waiting for transaction to be mined...")
	time.Sleep(30 * time.Second)

	// Get transaction receipt
	receipt, err := api.EthGetTransactionReceipt(ctx, txHash)
	if err != nil {
		log.Printf("[ERROR] Failed to get transaction receipt: %v", err)
		return nil
	}

	if receipt == nil {
		log.Printf("[ERROR] Transaction receipt is nil")
		return nil
	}

	// Assert transaction was mined successfully
	assert.Sometimes(receipt.Status == 1, "Transaction must be mined successfully", map[string]interface{}{"tx_hash": txHash})
	return nil
}

func sendEthLegacyTransaction(ctx context.Context, nodeConfig *resources.NodeConfig) error {
	log.Printf("[INFO] Starting ETH legacy transaction check on node '%s'...", nodeConfig.Name)
	key, ethAddr, deployer := resources.NewAccount()
	_, ethAddr2, _ := resources.NewAccount()

	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to Lotus node '%s': %w", nodeConfig.Name, err)
	}
	defer closer()

	defaultAddr, err := api.WalletDefaultAddress(ctx)
	if err != nil {
		return fmt.Errorf("failed to get default wallet address: %w", err)
	}

	resources.SendFunds(ctx, api, defaultAddr, deployer, types.FromFil(1000))
	time.Sleep(60 * time.Second)

	gasParams, err := json.Marshal(ethtypes.EthEstimateGasParams{Tx: ethtypes.EthCall{
		From:  &ethAddr,
		To:    &ethAddr2,
		Value: ethtypes.EthBigInt(big.NewInt(100)),
	}})
	if err != nil {
		return fmt.Errorf("failed to marshal gas params: %w", err)
	}

	gasLimit, err := api.EthEstimateGas(ctx, gasParams)
	if err != nil {
		log.Printf("[WARN] Failed to estimate gas, which might be expected: %v", err)
		return nil
	}

	tx := ethtypes.EthLegacyHomesteadTxArgs{
		Value:    big.NewInt(100),
		Nonce:    0,
		To:       &ethAddr2,
		GasPrice: types.NanoFil,
		GasLimit: int(gasLimit),
		V:        big.Zero(),
		R:        big.Zero(),
		S:        big.Zero(),
	}
	resources.SignLegacyHomesteadTransaction(&tx, key.PrivateKey)
	txHash := resources.SubmitTransaction(ctx, api, &tx)
	log.Printf("[INFO] Transaction submitted with hash: %s", txHash)

	if txHash == ethtypes.EmptyEthHash {
		log.Printf("[WARN] Transaction submission failed (empty hash), which might be expected.")
		return nil
	}
	log.Printf("[INFO] Transaction: %v", txHash)

	// Wait for transaction to be mined
	log.Printf("[INFO] Waiting for transaction to be mined...")
	time.Sleep(30 * time.Second)

	// Get transaction receipt
	receipt, err := api.EthGetTransactionReceipt(ctx, txHash)
	if err != nil {
		log.Printf("[WARN] Failed to get transaction receipt, which might be expected: %v", err)
		return nil
	}

	if receipt == nil {
		log.Printf("[WARN] Transaction receipt is nil, which might be expected.")
		return nil
	}

	log.Printf("[INFO] ETH legacy transaction check completed successfully")
	assert.Sometimes(receipt.Status == 1, "Transaction mined successfully", map[string]interface{}{"tx_hash": txHash})
	return nil
}

func performStateMismatch(ctx context.Context, nodeConfig *resources.NodeConfig) error {
	log.Printf("[INFO] Starting state mismatch check on node '%s'...", nodeConfig.Name)

	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to Lotus node '%s': %w", nodeConfig.Name, err)
	}
	defer closer()
	err = resources.StateMismatch(ctx, api)
	if err != nil {
		return fmt.Errorf("state mismatch check failed: %w", err)
	}

	return nil
}

func performSendConsensusFault(ctx context.Context) error {
	log.Println("[INFO] Attempting to send a consensus fault...")
	err := resources.SendConsensusFault(ctx)
	if err != nil {
		return fmt.Errorf("failed to send consensus fault: %w", err)
	}
	log.Println("[INFO] SendConsensusFault operation initiated. Check further logs for details.")
	return nil
}

func performEthMethodsCheck(ctx context.Context) error {
	log.Printf("[INFO] Starting ETH methods consistency check...")

	err := resources.CheckEthMethods(ctx)
	if err != nil {
		return fmt.Errorf("failed to create ETH methods checker: %w", err)
	}
	log.Printf("[INFO] ETH methods consistency check completed successfully")
	return nil
}

func performBlockFuzzing(ctx context.Context, nodeConfig *resources.NodeConfig) error {
	log.Printf("[INFO] Starting block fuzzing on node '%s'...", nodeConfig.Name)

	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to Lotus node '%s': %w", nodeConfig.Name, err)
	}
	defer closer()

	err = resources.FuzzBlockSubmission(ctx, api)
	if err != nil {
		return fmt.Errorf("block fuzzing failed: %w", err)
	}

	log.Printf("[INFO] Block fuzzing completed successfully")
	return nil
}

func performCheckBackfill(ctx context.Context, config *resources.Config) error {
	log.Println("[INFO] Starting chain index backfill check...")

	// Filter nodes to "Lotus1" and "Lotus2"
	nodeNames := []string{"Lotus1", "Lotus2"}
	var filteredNodes []resources.NodeConfig
	for _, node := range config.Nodes {
		for _, name := range nodeNames {
			if node.Name == name {
				filteredNodes = append(filteredNodes, node)
			}
		}
	}

	if len(filteredNodes) == 0 {
		return fmt.Errorf("no nodes matching '%s' or '%s' found in config", nodeNames[0], nodeNames[1])
	}

	err := resources.CheckChainBackfill(ctx, filteredNodes)
	if err != nil {
		return fmt.Errorf("chain backfill check failed: %w", err)
	}
	assert.Sometimes(true, "Chain index backfill check completed.", map[string]interface{}{"requirement": "Chain index backfill check completed."})
	log.Println("[INFO] Chain index backfill check completed.")
	return nil
}
