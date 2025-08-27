package eth

import (
	"bytes"
	"context"
	"encoding/hex"
	"log"
	"os"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources/connect"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/go-state-types/builtin/v10/eam"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build/buildconstants"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
	"github.com/filecoin-project/lotus/chain/wallet/key"
	"github.com/filecoin-project/lotus/lib/sigs"
	_ "github.com/filecoin-project/lotus/lib/sigs/delegated"
	_ "github.com/filecoin-project/lotus/lib/sigs/secp"
	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/crypto/sha3"
)

// DeployContractWithValue deploys a smart contract with a specified value transfer
// It handles contract initialization, message creation, and deployment confirmation
func DeployContractWithValue(ctx context.Context, api api.FullNode, sender address.Address, bytecode []byte, value big.Int) eam.CreateReturn {
	var result eam.CreateReturn

	method := builtin.MethodsEAM.CreateExternal
	initcode := abi.CborBytes(bytecode)

	params, Actorerr := actors.SerializeParams(&initcode)
	if Actorerr != nil {
		log.Printf("Failed to serialize contract initialization parameters: %v", Actorerr)
		return result
	}

	msg := &types.Message{
		To:     builtin.EthereumAddressManagerActorAddr,
		From:   sender,
		Value:  value,
		Method: method,
		Params: params,
	}

	smsg, err := api.MpoolPushMessage(ctx, msg, nil)
	if err != nil {
		log.Printf("Failed to push create contract message: %v", err)
		return result
	}
	time.Sleep(5 * time.Second)

	wait, err := api.StateWaitMsg(ctx, smsg.Cid(), 5, abi.ChainEpoch(-1), false)
	if err != nil {
		log.Printf("Error waiting for contract creation message: %v", err)
		return result
	}

	if !wait.Receipt.ExitCode.IsSuccess() {
		log.Printf("Contract installation failed with exit code: %v", wait.Receipt.ExitCode)
		return result
	}

	r := bytes.NewReader(wait.Receipt.Return)
	err = result.UnmarshalCBOR(r)
	if err != nil {
		log.Printf("Failed to unmarshal CBOR result: %v", err)
		return result
	}

	return result
}

// DeployContract deploys a smart contract with zero value transfer
// It's a convenience wrapper around DeployContractWithValue
func DeployContract(ctx context.Context, api api.FullNode, sender address.Address, bytecode []byte) eam.CreateReturn {
	return DeployContractWithValue(ctx, api, sender, bytecode, big.Zero())
}

// DeployContractFromFilenameWithValue deploys a contract from a file containing hex-encoded bytecode
// with a specified value transfer. Returns the sender and contract addresses
func DeployContractFromFilenameWithValue(ctx context.Context, api api.FullNode, binFilename string, value big.Int) (address.Address, address.Address) {
	// Check if file exists
	if _, err := os.Stat(binFilename); os.IsNotExist(err) {
		log.Printf("[ERROR] Contract file not found: %s", binFilename)
		return address.Address{}, address.Address{}
	}

	contractHex, err := os.ReadFile(binFilename)
	if err != nil {
		log.Printf("[ERROR] Failed to read contract file %s: %v", binFilename, err)
		return address.Address{}, address.Address{}
	}

	contractHex = bytes.TrimRight(contractHex, "\n")
	contract, err := hex.DecodeString(string(contractHex))
	if err != nil {
		log.Printf("[ERROR] Failed to decode hex string: %v", err)
		return address.Address{}, address.Address{}
	}

	fromAddr, err := api.WalletDefaultAddress(ctx)
	if err != nil || fromAddr.Empty() {
		log.Printf("[ERROR] No default wallet address found: %v", err)

		// Try to get any wallet address
		addresses, err := api.WalletList(ctx)
		if err != nil || len(addresses) == 0 {
			log.Printf("[ERROR] No wallet addresses available: %v", err)
			return address.Address{}, address.Address{}
		}

		// Use the first address
		fromAddr = addresses[0]
		log.Printf("[INFO] Using wallet address: %s", fromAddr)
	}

	// Verify address has funds
	balance, err := api.WalletBalance(ctx, fromAddr)
	if err != nil {
		log.Printf("[WARN] Failed to check wallet balance: %v", err)
	} else if balance.IsZero() {
		log.Printf("[WARN] Wallet has zero balance, deployment may fail")
	}

	result := DeployContractWithValue(ctx, api, fromAddr, contract, value)

	if result.ActorID == 0 {
		log.Printf("[ERROR] Contract deployment failed, got ActorID 0")
		return fromAddr, address.Address{}
	}

	idAddr, err := address.NewIDAddress(result.ActorID)
	if err != nil {
		log.Printf("[ERROR] Failed to create ID address from ActorID %d: %v", result.ActorID, err)
		return fromAddr, address.Address{}
	}

	log.Printf("[INFO] Contract deployed from %s to %s (Actor ID: %d)", fromAddr, idAddr, result.ActorID)
	return fromAddr, idAddr
}

// DeployContractFromFilename deploys a contract from a file containing hex-encoded bytecode
// with zero value transfer. Returns the sender and contract addresses
func DeployContractFromFilename(ctx context.Context, api api.FullNode, binFilename string) (address.Address, address.Address) {
	fromAddr, contractAddr := DeployContractFromFilenameWithValue(ctx, api, binFilename, big.Zero())

	return fromAddr, contractAddr
}

// InvokeSolidity calls a Solidity contract function with zero value transfer
// It's a convenience wrapper around InvokeSolidityWithValue
func InvokeSolidity(ctx context.Context, api api.FullNode, sender address.Address, target address.Address, selector []byte, inputData []byte) (*api.MsgLookup, error) {
	return InvokeSolidityWithValue(ctx, api, sender, target, selector, inputData, big.Zero())
}

// InvokeContractByFuncName calls a contract function using its function signature
// Returns the function's return data and message lookup information
func InvokeContractByFuncName(ctx context.Context, api api.FullNode, fromAddr address.Address, idAddr address.Address, funcSignature string, inputData []byte) ([]byte, *api.MsgLookup, error) {
	entryPoint := CalcFuncSignature(funcSignature)

	wait, err := InvokeSolidity(ctx, api, fromAddr, idAddr, entryPoint, inputData)
	if err != nil {
		log.Printf("[ERROR] Failed to invoke Solidity function: %v", err)
		return nil, wait, nil
	}

	if !wait.Receipt.ExitCode.IsSuccess() {
		replayResult, replayErr := api.StateReplay(ctx, types.EmptyTSK, wait.Message)
		if replayErr != nil {
			log.Printf("[ERROR] Failed to replay failed message: %v", replayErr)
			return nil, wait, nil
		}
		log.Printf("[ERROR] Invoke failed with error: %v", replayResult.Error)
		return nil, wait, nil
	}

	result, err := cbg.ReadByteArray(bytes.NewBuffer(wait.Receipt.Return), uint64(len(wait.Receipt.Return)))
	if err != nil {
		log.Printf("Failed to read return data from contract execution: %v", err)
	}
	return result, wait, err
}

// InvokeSolidityWithValue calls a Solidity contract function with a specified value transfer
// It handles parameter encoding, message creation, and execution confirmation
func InvokeSolidityWithValue(ctx context.Context, api api.FullNode, sender address.Address, target address.Address, selector []byte, inputData []byte, value big.Int) (*api.MsgLookup, error) {
	params := append(selector, inputData...)
	var buffer bytes.Buffer
	err := cbg.WriteByteArray(&buffer, params)
	if err != nil {
		log.Printf("[ERROR] Failed to write byte array to buffer: %v", err)
		return nil, nil
	}

	params = buffer.Bytes()

	msg := &types.Message{
		To:       target,
		From:     sender,
		Value:    value,
		Method:   builtin.MethodsEVM.InvokeContract,
		GasLimit: buildconstants.BlockGasLimit,
		Params:   params,
	}

	smsg, err := api.MpoolPushMessage(ctx, msg, nil)
	if err != nil {
		log.Printf("Failed to push message to invoke contract: %v", err)
		return nil, err
	}
	time.Sleep(5 * time.Second)

	wait, err := api.StateWaitMsg(ctx, smsg.Cid(), 5, abi.ChainEpoch(-1), false)
	if err != nil {
		log.Printf("Error waiting for invoke message to execute: %v", err)
		return nil, err
	}

	if !wait.Receipt.ExitCode.IsSuccess() {
		log.Print("Contract invocation failed")
		replayResult, err := api.StateReplay(ctx, types.EmptyTSK, wait.Message)
		log.Printf("StateReplay Error (err): %s", err)
		if replayResult != nil {
			log.Printf("StateReplay Error (replayResult.Error): %s", replayResult.Error)
			log.Printf("[ERROR] Invoke failed with error: %v", replayResult.Error)
			return nil, nil
		}
		log.Printf("[ERROR] Invoke failed and failed to replay: %v", err)
		return nil, nil
	}
	return wait, nil
}

// CalcFuncSignature calculates the 4-byte function selector for a Solidity function signature
// using Keccak-256 hashing according to the Solidity ABI specification
func CalcFuncSignature(funcName string) []byte {
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write([]byte(funcName))
	return hasher.Sum(nil)[:4]
}

// InputDataFromFrom converts a Filecoin address to its Ethereum format for use as input data
// Returns a 32-byte array with the Ethereum address right-aligned
func InputDataFromFrom(ctx context.Context, api api.FullNode, from address.Address) []byte {
	if from.Empty() {
		log.Printf("[ERROR] Cannot process empty 'from' address")
		return make([]byte, 32) // Return empty data instead of panicking
	}

	fromID, err := api.StateLookupID(ctx, from, types.EmptyTSK)
	if err != nil {
		log.Printf("[ERROR] Failed to lookup ID for address %s: %v", from, err)
		return make([]byte, 32) // Return empty data instead of panicking
	}

	senderEthAddr, err := ethtypes.EthAddressFromFilecoinAddress(fromID)
	if err != nil {
		log.Printf("[ERROR] Failed to convert address %s to Ethereum format: %v", fromID, err)
		return make([]byte, 32) // Return empty data instead of panicking
	}

	inputData := make([]byte, 32)
	copy(inputData[32-len(senderEthAddr):], senderEthAddr[:])
	return inputData
}

// SignTransaction signs an Ethereum 1559-style transaction with the provided private key
func SignTransaction(tx *ethtypes.Eth1559TxArgs, privKey []byte) {
	preimage, err := tx.ToRlpUnsignedMsg()
	if err != nil {
		log.Printf("Failed to convert transaction to RLP: %v", err)
		return
	}
	signature, err := sigs.Sign(crypto.SigTypeDelegated, privKey, preimage)
	if err != nil {
		log.Printf("Failed to sign transaction: %v", err)
		return
	}
	err = tx.InitialiseSignature(*signature)
	if err != nil {
		log.Printf("Failed to initialise signature: %v", err)
		return
	}
}

// SubmitTransaction submits a signed Ethereum transaction to the network
// Returns the transaction hash
func SubmitTransaction(ctx context.Context, api api.FullNode, tx ethtypes.EthTransaction) ethtypes.EthHash {
	signed, err := tx.ToRlpSignedMsg()
	if err != nil {
		log.Printf("Failed to convert transaction to RLP: %v", err)
		return ethtypes.EthHash{}
	}
	txHash, err := api.EthSendRawTransaction(ctx, signed)
	if err != nil {
		log.Printf("Failed to send transaction: %v", err)
		return ethtypes.EthHash{}
	}
	return txHash
}

// NewAccount creates a new Ethereum-compatible account
// Returns the private key, Ethereum address, and Filecoin address
func NewAccount() (*key.Key, ethtypes.EthAddress, address.Address) {
	// Generate a secp256k1 key; this will back the Ethereum identity.
	key, err := key.GenerateKey(types.KTSecp256k1)
	if err != nil {
		log.Printf("Failed to generate key: %v", err)
		return nil, ethtypes.EthAddress{}, address.Address{}
	}

	ethAddr, err := ethtypes.EthAddressFromPubKey(key.PublicKey)
	if err != nil {
		log.Printf("Failed to generate Ethereum address: %v", err)
		return nil, ethtypes.EthAddress{}, address.Address{}
	}

	ea, err := ethtypes.CastEthAddress(ethAddr)
	if err != nil {
		log.Printf("Failed to cast Ethereum address: %v", err)
		return nil, ethtypes.EthAddress{}, address.Address{}
	}

	addr, err := ea.ToFilecoinAddress()
	if err != nil {
		log.Printf("Failed to convert Ethereum address to Filecoin address: %v", err)
		return nil, ethtypes.EthAddress{}, address.Address{}
	}
	return key, *(*ethtypes.EthAddress)(ethAddr), addr
}

// SignLegacyHomesteadTransaction signs an Ethereum legacy (pre-1559) transaction
// with the provided private key
func SignLegacyHomesteadTransaction(tx *ethtypes.EthLegacyHomesteadTxArgs, privKey []byte) {
	preimage, err := tx.ToRlpUnsignedMsg()
	if err != nil {
		log.Printf("Failed to convert transaction to RLP: %v", err)
		return
	}

	// sign the RLP payload
	signature, err := sigs.Sign(crypto.SigTypeDelegated, privKey, preimage)
	if err != nil {
		log.Printf("Failed to sign transaction: %v", err)
		return
	}

	signature.Data = append([]byte{ethtypes.EthLegacyHomesteadTxSignaturePrefix}, signature.Data...)
	signature.Data[len(signature.Data)-1] += 27

	err = tx.InitialiseSignature(*signature)
	if err != nil {
		log.Printf("Failed to initialise signature: %v", err)
		return
	}
}

// PerformDeploySimpleCoin deploys SimpleCoin contract on a specified node
func PerformDeploySimpleCoin(ctx context.Context, nodeConfig *connect.NodeConfig, contractPath string) error {
	log.Printf("[INFO] Deploying SimpleCoin contract on node %s from %s", nodeConfig.Name, contractPath)

	// Verify contract file exists first
	if _, err := os.Stat(contractPath); os.IsNotExist(err) {
		log.Printf("[ERROR] Contract file not found: %s", contractPath)
		return nil
	}

	api, closer, err := connect.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return nil
	}
	defer closer()

	return connect.RetryOperation(ctx, func() error {
		// Check if we have a default wallet address
		defaultAddr, err := api.WalletDefaultAddress(ctx)
		if err != nil || defaultAddr.Empty() {
			log.Printf("[WARN] No default wallet address found, attempting to get or create one")

			// Get all available addresses
			addresses, err := api.WalletList(ctx)
			if err != nil {
				log.Printf("[ERROR] Failed to list wallet addresses: %v", err)
				return nil
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
					log.Printf("[ERROR] Failed to create new wallet address: %v", err)
					return nil
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
		fromAddr, contractAddr := DeployContractFromFilename(ctx, api, contractPath)

		if fromAddr.Empty() || contractAddr.Empty() {
			log.Printf("[WARN] Deployment returned empty addresses, will retry")
			return err
		}

		log.Printf("[INFO] Contract deployed from %s to %s", fromAddr, contractAddr)

		// Generate input data for owner's address
		inputData := InputDataFromFrom(ctx, api, fromAddr)
		if len(inputData) == 0 {
			log.Printf("[WARN] Failed to generate input data, will retry")
			return err
		}

		// Invoke contract for owner's balance
		log.Printf("[INFO] Checking owner's balance")
		result, _, err := InvokeContractByFuncName(ctx, api, fromAddr, contractAddr, "getBalance(address)", inputData)
		if err != nil {
			log.Printf("[WARN] Failed to retrieve owner's balance: %v", err)
		} else {
			log.Printf("[INFO] Owner's balance: %x", result)
		}

		return nil
	}, "SimpleCoin contract deployment operation")
}

// PerformDeployMCopy deploys MCopy contract on a specified node
func PerformDeployMCopy(ctx context.Context, nodeConfig *connect.NodeConfig, contractPath string) error {
	log.Printf("Deploying and invoking MCopy contract on node '%s'...", nodeConfig.Name)

	api, closer, err := connect.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return nil
	}
	defer closer()

	return connect.RetryOperation(ctx, func() error {
		fromAddr, contractAddr := DeployContractFromFilename(ctx, api, contractPath)

		hexString := "000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000087465737464617461000000000000000000000000000000000000000000000000"
		inputArgument, err := hex.DecodeString(hexString)
		if err != nil {
			log.Printf("[ERROR] Failed to decode input argument: %v", err)
			return nil
		}

		result, _, err := InvokeContractByFuncName(ctx, api, fromAddr, contractAddr, "optimizedCopy(bytes)", inputArgument)
		if err != nil {
			log.Printf("[ERROR] Failed to invoke MCopy contract: %v", err)
			return nil
		}
		if bytes.Equal(result, inputArgument) {
			log.Printf("MCopy invocation result matches the input argument. No change in the output.")
		} else {
			log.Printf("MCopy invocation result: %x\n", result)
		}
		return nil
	}, "MCopy contract deployment operation")
}

// PerformDeployTStore deploys TStore contract on a specified node
func PerformDeployTStore(ctx context.Context, nodeConfig *connect.NodeConfig, contractPath string) error {
	log.Printf("Deploying and invoking TStore contract on node '%s'...", nodeConfig.Name)

	api, closer, err := connect.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return nil
	}
	defer closer()

	return connect.RetryOperation(ctx, func() error {
		// Deploy the contract
		fromAddr, contractAddr := DeployContractFromFilename(ctx, api, contractPath)
		if fromAddr.Empty() || contractAddr.Empty() {
			log.Printf("[ERROR] Failed to deploy initial contract instance")
			return nil
		}

		inputData := make([]byte, 0)

		// Run initial tests
		_, _, err = InvokeContractByFuncName(ctx, api, fromAddr, contractAddr, "runTests()", inputData)
		if err != nil {
			log.Printf("[ERROR] Failed to invoke runTests(): %v", err)
			return nil
		}

		// Validate lifecycle in subsequent transactions
		_, _, err = InvokeContractByFuncName(ctx, api, fromAddr, contractAddr, "testLifecycleValidationSubsequentTransaction()", inputData)
		if err != nil {
			log.Printf("[ERROR] Failed to invoke testLifecycleValidationSubsequentTransaction(): %v", err)
			return nil
		}

		// Deploy a second contract instance for further testing
		fromAddr, contractAddr2 := DeployContractFromFilename(ctx, api, contractPath)
		if fromAddr.Empty() || contractAddr2.Empty() {
			log.Printf("[ERROR] Failed to deploy second contract instance")
			return nil
		}

		inputDataContract := InputDataFromFrom(ctx, api, contractAddr2)
		if len(inputDataContract) == 0 {
			log.Printf("[ERROR] Failed to generate input data for contract address")
			return nil
		}

		// Test re-entry scenarios
		_, _, err = InvokeContractByFuncName(ctx, api, fromAddr, contractAddr, "testReentry(address)", inputDataContract)
		if err != nil {
			log.Printf("[ERROR] Failed to invoke testReentry(): %v", err)
			return nil
		}

		// Test nested contract interactions
		_, _, err = InvokeContractByFuncName(ctx, api, fromAddr, contractAddr, "testNestedContracts(address)", inputDataContract)
		if err != nil {
			log.Printf("[ERROR] Failed to invoke testNestedContracts(): %v", err)
			return nil
		}

		log.Printf("TStore contract successfully deployed and tested on node '%s'.", nodeConfig.Name)
		return nil
	}, "TStore contract deployment and testing operation")
}
