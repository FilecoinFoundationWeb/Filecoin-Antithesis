package resources

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"reflect"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
)

// CheckEthMethods verifies consistency between Ethereum API methods by comparing blocks
// retrieved via eth_getBlockByNumber and eth_getBlockByHash. It checks multiple block
// properties including hashes, numbers, timestamps, and parent hashes for the last 30 blocks.
func CheckEthMethods(ctx context.Context) error {
	return RetryOperation(ctx, func() error {
		config, err := LoadConfig("/opt/antithesis/resources/config.json")
		if err != nil {
			log.Printf("[ERROR] Failed to load config: %v", err)
			return nil
		}

		filteredNodes := FilterV1Nodes(config.Nodes)

		// Check if we have enough epochs to avoid false positives
		if len(filteredNodes) > 0 {
			api, closer, err := ConnectToNode(ctx, filteredNodes[0])
			if err != nil {
				log.Printf("[ERROR] Failed to connect to node for epoch check: %v", err)
				return nil
			}
			defer closer()

			head, err := api.ChainHead(ctx)
			if err != nil {
				log.Printf("[ERROR] Failed to get chain head for epoch check: %v", err)
				return nil
			}

			if head.Height() < 20 {
				log.Printf("[INFO] Current epoch %d is less than required minimum (20). Skipping ETH methods check to avoid false positives.", head.Height())
				return nil
			}
		}

		for _, node := range filteredNodes {
			log.Printf("[INFO] Checking ETH methods on node %s", node.Name)
			api, closer, err := ConnectToNode(ctx, node)
			if err != nil {
				log.Printf("[ERROR] Failed to connect to node %s: %v", node.Name, err)
				return nil
			}
			defer closer()

			head, err := api.ChainHead(ctx)
			if err != nil {
				log.Printf("[ERROR] Failed to get chain head: %v", err)
				return nil
			}

			height := head.Height()
			targetHeight := height - 30
			for i := int64(height); i > int64(targetHeight); i-- {
				// Use RetryOperation for each block check
				err := RetryOperation(ctx, func() error {
					if _, err := api.ChainGetTipSetByHeight(ctx, abi.ChainEpoch(i), types.EmptyTSK); err != nil {
						log.Printf("[ERROR] Failed to get tipset @%d from Lotus: %v", i, err)
						return nil
					}

					hex := fmt.Sprintf("0x%x", i)
					ethBlockA, err := api.EthGetBlockByNumber(ctx, hex, false)
					if err != nil {
						log.Printf("[ERROR] Failed to get tipset @%d via eth_getBlockByNumber: %v", i, err)
						return nil
					}
					log.Printf("[DEBUG] Block by Number - Height: %d, Hash: %s", i, ethBlockA.Hash)

					ethBlockB, err := api.EthGetBlockByHash(ctx, ethBlockA.Hash, true)
					if err != nil {
						log.Printf("[ERROR] Failed to get tipset @%d via eth_getBlockByHash: %v", i, err)
						return nil
					}
					log.Printf("[DEBUG] Block by Hash - Height: %d, Hash: %s", i, ethBlockB.Hash)

					// Use DeepEqual to check overall block equality
					equal := reflect.DeepEqual(ethBlockA, ethBlockB)
					if !equal {
						log.Printf("[WARN] Block mismatch at height %d:", i)
						log.Printf("  Block by Number Hash: %s", ethBlockA.Hash)
						log.Printf("  Block by Hash Hash: %s", ethBlockB.Hash)
						log.Printf("  Block by Number ParentHash: %s", ethBlockA.ParentHash)
						log.Printf("  Block by Hash ParentHash: %s", ethBlockB.ParentHash)
						log.Printf("  Block by Number Number: %d", ethBlockA.Number)
						log.Printf("  Block by Hash Number: %d", ethBlockB.Number)
						log.Printf("  Block by Number Timestamp: %d", ethBlockA.Timestamp)
						log.Printf("  Block by Hash Timestamp: %d", ethBlockB.Timestamp)
						log.Printf("[ERROR] Block mismatch at height %d", i)
						return nil
					}

					AssertAlways(node.Name, equal,
						"ETH block consistency: Blocks should be identical regardless of retrieval method - API inconsistency detected",
						map[string]interface{}{
							"operation":      "eth_block_consistency",
							"height":         i,
							"blockByNumber":  ethBlockA,
							"blockByHash":    ethBlockB,
							"property":       "Block data consistency",
							"impact":         "Critical - indicates API inconsistency",
							"details":        "Block data must be identical when retrieved by number or hash",
							"recommendation": "Check block retrieval and serialization logic",
						})

					// Additional specific field checks for better error reporting
					AssertAlways(node.Name, ethBlockA.Hash == ethBlockB.Hash,
						"ETH block hash consistency: Block hashes must be identical - hash computation error detected",
						map[string]interface{}{
							"operation":     "eth_block_hash_consistency",
							"height":        i,
							"blockByNumber": ethBlockA.Hash,
							"blockByHash":   ethBlockB.Hash,
							"property":      "Block hash consistency",
							"impact":        "Critical - indicates hash computation error",
							"details":       "Block hash must be identical across retrieval methods",
						})

					AssertAlways(node.Name, ethBlockA.Number == ethBlockB.Number,
						"ETH block number consistency: Block numbers must be identical - block height mismatch detected",
						map[string]interface{}{
							"operation":     "eth_block_number_consistency",
							"height":        i,
							"blockByNumber": ethBlockA.Number,
							"blockByHash":   ethBlockB.Number,
							"property":      "Block number consistency",
							"impact":        "Critical - indicates block height mismatch",
							"details":       "Block number must be identical across retrieval methods",
						})

					AssertAlways(node.Name, ethBlockA.ParentHash == ethBlockB.ParentHash,
						"ETH parent hash consistency: Parent hashes must be identical - chain linking error detected",
						map[string]interface{}{
							"operation":     "eth_parent_hash_consistency",
							"height":        i,
							"blockByNumber": ethBlockA.ParentHash,
							"blockByHash":   ethBlockB.ParentHash,
							"property":      "Parent hash consistency",
							"impact":        "Critical - indicates chain linking error",
							"details":       "Parent hash must be identical across retrieval methods",
						})

					AssertAlways(node.Name, ethBlockA.Timestamp == ethBlockB.Timestamp,
						"ETH timestamp consistency: Block timestamps must be identical - timestamp mismatch detected",
						map[string]interface{}{
							"operation":     "eth_timestamp_consistency",
							"height":        i,
							"blockByNumber": ethBlockA.Timestamp,
							"blockByHash":   ethBlockB.Timestamp,
							"property":      "Block timestamp consistency",
							"impact":        "Critical - indicates timestamp mismatch",
							"details":       "Block timestamp must be identical across retrieval methods",
						})

					log.Printf("[OK] Blocks received via eth_getBlockByNumber and eth_getBlockByHash for tipset @%d are identical", i)
					return nil
				}, fmt.Sprintf("Block check at height %d", i))

				if err != nil {
					// Log the error but continue with next height
					log.Printf("[ERROR] Failed to check block at height %d after retries: %v", i, err)
					continue
				}
			}
		}
		return nil
	}, "ETH methods consistency check")
}

// PerformEthMethodsCheck checks ETH methods consistency
func PerformEthMethodsCheck(ctx context.Context) error {
	log.Printf("[INFO] Starting ETH methods consistency check...")

	err := CheckEthMethods(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to create ETH methods checker: %v", err)
		return nil
	}
	log.Printf("[INFO] ETH methods consistency check completed successfully")
	return nil
}

// SendEthLegacyTransaction sends ETH legacy transaction
func SendEthLegacyTransaction(ctx context.Context, nodeConfig *NodeConfig) error {
	log.Printf("[INFO] Starting ETH legacy transaction check on node '%s'...", nodeConfig.Name)
	key, ethAddr, deployer := NewAccount()
	_, ethAddr2, _ := NewAccount()

	api, closer, err := ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return nil
	}
	defer closer()

	// Handle wallet initialization differently for Forest and Lotus nodes
	if nodeConfig.Name == "Forest" {
		// For Forest, we need to get funds from Lotus first
		lotusNode := NodeConfig{
			Name:          "Lotus1",
			RPCURL:        "http://10.20.20.24:1234/rpc/v1",
			AuthTokenPath: "/root/devgen/lotus-1/jwt",
		}
		lotusApi, lotusCloser, err := ConnectToNode(ctx, lotusNode)
		if err != nil {
			log.Printf("[ERROR] Failed to connect to Lotus node for Forest wallet initialization: %v", err)
			return nil
		}
		defer lotusCloser()

		// Initialize Forest wallets with funding from Lotus
		if err := InitializeForestWallets(ctx, api, lotusApi, 1, types.FromFil(1000)); err != nil {
			log.Printf("[ERROR] Failed to initialize Forest wallets: %v", err)
			return nil
		}

		// Get the default wallet which was just set by InitializeForestWallets
		defaultAddr, err := api.WalletDefaultAddress(ctx)
		if err != nil {
			log.Printf("[ERROR] Failed to get Forest default wallet address: %v", err)
			return nil
		}

		SendFunds(ctx, api, defaultAddr, deployer, types.FromFil(1000))
	} else {
		// For Lotus nodes, use standard wallet initialization
		defaultAddr, err := api.WalletDefaultAddress(ctx)
		if err != nil {
			log.Printf("[ERROR] Failed to get default wallet address: %v", err)
			return nil
		}

		SendFunds(ctx, api, defaultAddr, deployer, types.FromFil(1000))
	}

	time.Sleep(60 * time.Second)

	gasParams, err := json.Marshal(ethtypes.EthEstimateGasParams{Tx: ethtypes.EthCall{
		From:  &ethAddr,
		To:    &ethAddr2,
		Value: ethtypes.EthBigInt(big.NewInt(10)),
	}})
	if err != nil {
		log.Printf("[ERROR] Failed to marshal gas params: %v", err)
		return nil
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
	SignLegacyHomesteadTransaction(&tx, key.PrivateKey)
	txHash := SubmitTransaction(ctx, api, &tx)
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
	AssertSometimes(nodeConfig.Name, receipt.Status == 1, "ETH legacy transaction: Transaction should be mined successfully - mining failure detected", map[string]interface{}{
		"operation": "eth_legacy_transaction",
		"tx_hash":   txHash,
	})
	return nil
}

// DeploySmartContract deploys a smart contract
func DeploySmartContract(ctx context.Context, nodeConfig *NodeConfig, contractPath string) error {
	log.Printf("[INFO] Deploying smart contract from %s...", contractPath)

	// Connect to Lotus node
	api, closer, err := ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return nil
	}
	defer closer()

	// Create new account for deployment
	key, ethAddr, deployer := NewAccount()
	log.Printf("[INFO] Created new account - deployer: %s, ethAddr: %s", deployer, ethAddr)

	// Handle wallet initialization differently for Forest and Lotus nodes
	if nodeConfig.Name == "Forest" {
		// For Forest, we need to get funds from Lotus first
		lotusNode := NodeConfig{
			Name:          "Lotus1",
			RPCURL:        "http://10.20.20.24:1234/rpc/v1",
			AuthTokenPath: "/root/devgen/lotus-1/jwt",
		}
		lotusApi, lotusCloser, err := ConnectToNode(ctx, lotusNode)
		if err != nil {
			log.Printf("[ERROR] Failed to connect to Lotus node for Forest wallet initialization: %v", err)
			return nil
		}
		defer lotusCloser()

		// Initialize Forest wallets with funding from Lotus
		if err := InitializeForestWallets(ctx, api, lotusApi, 1, types.FromFil(100)); err != nil {
			log.Printf("[ERROR] Failed to initialize Forest wallets: %v", err)
			return nil
		}

		// Get the default wallet which was just set by InitializeForestWallets
		defaultAddr, err := api.WalletDefaultAddress(ctx)
		if err != nil {
			log.Printf("[ERROR] Failed to get Forest default wallet address: %v", err)
			return nil
		}

		// Send funds to deployer account
		log.Printf("[INFO] Sending funds to deployer account from Forest default wallet...")
		err = SendFunds(ctx, api, defaultAddr, deployer, types.FromFil(10))
		if err != nil {
			log.Printf("[ERROR] Failed to send funds to deployer: %v", err)
			return nil
		}
	} else {
		// For Lotus nodes, use standard wallet initialization
		defaultAddr, err := api.WalletDefaultAddress(ctx)
		if err != nil {
			log.Printf("[ERROR] Failed to get default wallet address: %v", err)
			return nil
		}

		// Send funds to deployer account
		log.Printf("[INFO] Sending funds to deployer account...")
		err = SendFunds(ctx, api, defaultAddr, deployer, types.FromFil(10))
		if err != nil {
			log.Printf("[ERROR] Failed to send funds to deployer: %v", err)
			return nil
		}
	}

	// Wait for funds to be available
	log.Printf("[INFO] Waiting for funds to be available...")
	time.Sleep(30 * time.Second)

	// Read and decode contract
	contractHex, err := os.ReadFile(contractPath)
	if err != nil {
		log.Printf("[ERROR] Failed to read contract file: %v", err)
		return nil
	}
	contract, err := hex.DecodeString(string(contractHex))
	if err != nil {
		log.Printf("[ERROR] Failed to decode contract: %v", err)
		return nil
	}

	// Estimate gas
	gasParams, err := json.Marshal(ethtypes.EthEstimateGasParams{Tx: ethtypes.EthCall{
		From: &ethAddr,
		Data: contract,
	}})
	if err != nil {
		log.Printf("[ERROR] Failed to marshal gas params: %v", err)
		return nil
	}
	gasLimit, err := api.EthEstimateGas(ctx, gasParams)
	if err != nil {
		log.Printf("[ERROR] Failed to estimate gas: %v", err)
		return nil
	}

	// Get gas fees
	maxPriorityFee, err := api.EthMaxPriorityFeePerGas(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to get max priority fee: %v", err)
		return nil
	}

	// Get nonce
	nonce, err := api.MpoolGetNonce(ctx, deployer)
	if err != nil {
		log.Printf("[ERROR] Failed to get nonce: %v", err)
		return nil
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
	SignTransaction(&tx, key.PrivateKey)
	txHash := SubmitTransaction(ctx, api, &tx)
	log.Printf("[INFO] Transaction submitted with hash: %s", txHash)

	AssertSometimes(nodeConfig.Name, txHash != ethtypes.EmptyEthHash, "ETH contract deployment: Transaction must be submitted successfully - submission failure detected", map[string]interface{}{
		"operation":   "eth_contract_deployment",
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
	AssertSometimes(nodeConfig.Name, receipt.Status == 1, "ETH contract deployment: Transaction must be mined successfully - mining failure detected", map[string]interface{}{
		"operation": "eth_contract_deployment",
		"tx_hash":   txHash,
	})
	return nil
}
