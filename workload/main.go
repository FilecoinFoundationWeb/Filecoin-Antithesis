package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	mpoolfuzz "github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources/mpool-fuzz"
	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
	"github.com/urfave/cli/v2"
)

var config *resources.Config

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("[INFO] Starting workload...")

	app := &cli.App{
		Name:  "workload",
		Usage: "Filecoin testing workload",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Value:   "/opt/antithesis/resources/config.json",
				Usage:   "Path to config JSON file",
				EnvVars: []string{"WORKLOAD_CONFIG"},
			},
		},
		Before: func(c *cli.Context) error {
			// Load configuration
			var err error
			config, err = resources.LoadConfig(c.String("config"))
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			return nil
		},
		Commands: []*cli.Command{
			walletCommands(),
			networkCommands(),
			contractCommands(),
			mempoolCommands(),
			consensusCommands(),
			monitoringCommands(),
			chainCommands(),
			stateCommands(),
			stressCommands(),
			rpcCommands(),
			ethCommands(),
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Printf("[ERROR] %v", err)
		os.Exit(1)
	}
}

func getNodeConfig(c *cli.Context) (*resources.NodeConfig, error) {
	nodeName := c.String("node")
	if nodeName == "" {
		return nil, fmt.Errorf("node name is required")
	}

	for i := range config.Nodes {
		if config.Nodes[i].Name == nodeName {
			return &config.Nodes[i], nil
		}
	}
	return nil, fmt.Errorf("node '%s' not found in config", nodeName)
}

func walletCommands() *cli.Command {
	nodeFlag := &cli.StringFlag{
		Name:     "node",
		Usage:    "node name from config.json (Lotus1 or Lotus2)",
		Required: true,
	}

	return &cli.Command{
		Name:  "wallet",
		Usage: "Wallet management operations",
		Subcommands: []*cli.Command{
			{
				Name:  "create",
				Usage: "Create new wallets",
				Flags: []cli.Flag{
					nodeFlag,
					&cli.IntFlag{
						Name:  "count",
						Value: 1,
						Usage: "Number of wallets to create",
					},
				},
				Action: func(c *cli.Context) error {
					nodeConfig, err := getNodeConfig(c)
					if err != nil {
						return err
					}
					return performCreateOperation(c.Context, nodeConfig, c.Int("count"), abi.NewTokenAmount(1000000000000000))
				},
			},
			{
				Name:  "delete",
				Usage: "Delete wallets",
				Flags: []cli.Flag{
					nodeFlag,
				},
				Action: func(c *cli.Context) error {
					nodeConfig, err := getNodeConfig(c)
					if err != nil {
						return err
					}
					return performDeleteOperation(c.Context, nodeConfig)
				},
			},
		},
	}
}

func networkCommands() *cli.Command {
	nodeFlag := &cli.StringFlag{
		Name:     "node",
		Usage:    "node name from config.json (Lotus1 or Lotus2)",
		Required: true,
	}

	return &cli.Command{
		Name:  "network",
		Usage: "Network testing operations",
		Subcommands: []*cli.Command{
			{
				Name:  "connect",
				Usage: "Connect node to other nodes",
				Flags: []cli.Flag{
					nodeFlag,
				},
				Action: func(c *cli.Context) error {
					nodeConfig, err := getNodeConfig(c)
					if err != nil {
						return err
					}
					log.Printf("Connecting node '%s' to other nodes...", nodeConfig.Name)
					api, closer, err := resources.ConnectToNode(c.Context, *nodeConfig)
					if err != nil {
						return fmt.Errorf("failed to connect to Lotus node '%s': %w", nodeConfig.Name, err)
					}
					defer closer()

					lotusNodes := resources.FilterLotusNodes(config.Nodes)
					if err := resources.ConnectToOtherNodes(c.Context, api, *nodeConfig, lotusNodes); err != nil {
						return fmt.Errorf("failed to connect node '%s' to other nodes: %w", nodeConfig.Name, err)
					}
					log.Printf("Node '%s' connected successfully", nodeConfig.Name)
					return nil
				},
			},
			{
				Name:  "disconnect",
				Usage: "Disconnect node from other nodes",
				Flags: []cli.Flag{
					nodeFlag,
				},
				Action: func(c *cli.Context) error {
					nodeConfig, err := getNodeConfig(c)
					if err != nil {
						return err
					}
					log.Printf("Disconnecting node '%s' from other nodes...", nodeConfig.Name)
					api, closer, err := resources.ConnectToNode(c.Context, *nodeConfig)
					if err != nil {
						return fmt.Errorf("failed to connect to Lotus node '%s': %w", nodeConfig.Name, err)
					}
					defer closer()

					if err := resources.DisconnectFromOtherNodes(c.Context, api); err != nil {
						return fmt.Errorf("failed to disconnect node '%s' from other nodes: %w", nodeConfig.Name, err)
					}
					log.Printf("Node '%s' disconnected successfully", nodeConfig.Name)
					return nil
				},
			},
			{
				Name:  "reorg",
				Usage: "Simulate a reorg by disconnecting, waiting, and reconnecting",
				Flags: []cli.Flag{
					nodeFlag,
					&cli.BoolFlag{
						Name:  "check-consensus",
						Usage: "Check for running consensus scripts and exit early if detected",
						Value: true,
					},
				},
				Action: func(c *cli.Context) error {
					// Check for running consensus scripts if the flag is enabled
					if c.Bool("check-consensus") {
						isRunning, err := resources.IsConsensusOrEthScriptRunning()
						if err != nil {
							log.Printf("[WARN] Failed to check for consensus/eth scripts: %v", err)
						} else if isRunning {
							log.Printf("[INFO] Consensus/ETH scripts detected running. Exiting reorg simulation early to avoid interference.")
							return nil
						}
					}

					nodeConfig, err := getNodeConfig(c)
					if err != nil {
						return err
					}
					log.Printf("Simulating reorg for node '%s'...", nodeConfig.Name)
					api, closer, err := resources.ConnectToNode(c.Context, *nodeConfig)
					if err != nil {
						return fmt.Errorf("failed to connect to Lotus node '%s': %w", nodeConfig.Name, err)
					}
					defer closer()

					if err := resources.SimulateReorg(c.Context, api); err != nil {
						log.Printf("failed to simulate reorg for node '%s': %w", nodeConfig.Name, err)
						return nil
					}
					log.Printf("Reorg simulation completed successfully for node '%s'", nodeConfig.Name)
					return nil
				},
			},
		},
	}
}

func mempoolCommands() *cli.Command {
	nodeFlag := &cli.StringFlag{
		Name:     "node",
		Usage:    "node name from config.json (Lotus1 or Lotus2)",
		Required: true,
	}

	return &cli.Command{
		Name:  "mempool",
		Usage: "Mempool testing operations",
		Subcommands: []*cli.Command{
			{
				Name:  "fuzz",
				Usage: "Run mempool fuzzing",
				Flags: []cli.Flag{
					nodeFlag,
					&cli.IntFlag{
						Name:  "count",
						Value: 100,
						Usage: "Number of transactions to perform",
					},
					&cli.IntFlag{
						Name:  "concurrency",
						Value: 5,
						Usage: "Number of concurrent operations",
					},
					&cli.StringFlag{
						Name:  "strategy",
						Value: "standard",
						Usage: "Fuzzing strategy (standard, chained)",
					},
				},
				Action: func(c *cli.Context) error {
					nodeConfig, err := getNodeConfig(c)
					if err != nil {
						return err
					}
					return performMempoolFuzz(
						c.Context,
						nodeConfig,
						c.Int("count"),
						c.Int("concurrency"),
						c.String("strategy"),
					)
				},
			},
			{
				Name:  "chained",
				Usage: "Run chained transaction mempool fuzzing",
				Flags: []cli.Flag{
					nodeFlag,
					&cli.IntFlag{
						Name:  "count",
						Value: 100,
						Usage: "Number of transactions to perform",
					},
					&cli.IntFlag{
						Name:  "concurrency",
						Value: 5,
						Usage: "Number of concurrent operations",
					},
				},
				Action: func(c *cli.Context) error {
					nodeConfig, err := getNodeConfig(c)
					if err != nil {
						return err
					}
					return performMempoolFuzz(
						c.Context,
						nodeConfig,
						c.Int("count"),
						c.Int("concurrency"),
						"chained",
					)
				},
			},
			{
				Name:  "track",
				Usage: "Track mempool size over time",
				Flags: []cli.Flag{
					nodeFlag,
					&cli.DurationFlag{
						Name:  "duration",
						Value: 5 * time.Minute,
						Usage: "Duration to track mempool (e.g., 5m, 10m, 1h)",
					},
					&cli.DurationFlag{
						Name:  "interval",
						Value: 5 * time.Second,
						Usage: "Interval between measurements (e.g., 1s, 5s, 30s)",
					},
				},
				Action: func(c *cli.Context) error {
					nodeConfig, err := getNodeConfig(c)
					if err != nil {
						return err
					}
					return performMempoolTracking(
						c.Context,
						nodeConfig,
						c.Duration("duration"),
						c.Duration("interval"),
					)
				},
			},
			{
				Name:  "spam",
				Usage: "Send valid spam transactions between wallets on all nodes",
				Action: func(c *cli.Context) error {
					return performSpamOperation(c.Context, config)
				},
			},
		},
	}
}

func contractCommands() *cli.Command {
	nodeFlag := &cli.StringFlag{
		Name:     "node",
		Usage:    "node name from config.json (Lotus1 or Lotus2)",
		Required: true,
	}

	const (
		simpleCoinPath = "/opt/antithesis/resources/smart-contracts/SimpleCoin.hex"
		mcopyPath      = "/opt/antithesis/resources/smart-contracts/MCopy.hex"
		tstoragePath   = "/opt/antithesis/resources/smart-contracts/TransientStorage.hex"
	)

	return &cli.Command{
		Name:  "contracts",
		Usage: "Smart contract operations",
		Subcommands: []*cli.Command{
			{
				Name:  "deploy-simple-coin",
				Usage: "Deploy SimpleCoin contract",
				Flags: []cli.Flag{
					nodeFlag,
				},
				Action: func(c *cli.Context) error {
					nodeConfig, err := getNodeConfig(c)
					if err != nil {
						return err
					}
					return performDeploySimpleCoin(c.Context, nodeConfig, simpleCoinPath)
				},
			},
			{
				Name:  "deploy-mcopy",
				Usage: "Deploy MCopy contract",
				Flags: []cli.Flag{
					nodeFlag,
				},
				Action: func(c *cli.Context) error {
					nodeConfig, err := getNodeConfig(c)
					if err != nil {
						return err
					}
					return performDeployMCopy(c.Context, nodeConfig, mcopyPath)
				},
			},
			{
				Name:  "deploy-tstorage",
				Usage: "Deploy Transient Storage contract",
				Flags: []cli.Flag{
					nodeFlag,
				},
				Action: func(c *cli.Context) error {
					nodeConfig, err := getNodeConfig(c)
					if err != nil {
						return err
					}
					return performDeployTStore(c.Context, nodeConfig, tstoragePath)
				},
			},
		},
	}
}

func consensusCommands() *cli.Command {
	return &cli.Command{
		Name:  "consensus",
		Usage: "Consensus testing operations",
		Subcommands: []*cli.Command{
			{
				Name:  "check",
				Usage: "Check consensus between nodes",
				Flags: []cli.Flag{
					&cli.Int64Flag{
						Name:  "height",
						Value: 0,
						Usage: "Chain height to check consensus at (0 for current height)",
					},
				},
				Action: func(c *cli.Context) error {
					return performConsensusCheck(c.Context, config, c.Int64("height"))
				},
			},
			{
				Name:  "fault",
				Usage: "Send consensus fault",
				Action: func(c *cli.Context) error {
					return performSendConsensusFault(c.Context)
				},
			},
			{
				Name:  "finalized",
				Usage: "Check finalized tipsets",
				Action: func(c *cli.Context) error {
					return performCheckFinalizedTipsets(c.Context)
				},
			},
		},
	}
}

func monitoringCommands() *cli.Command {
	return &cli.Command{
		Name:  "monitor",
		Usage: "Monitoring operations",
		Subcommands: []*cli.Command{
			{
				Name:  "peers",
				Usage: "Check peer connections",
				Action: func(c *cli.Context) error {
					return checkPeers()
				},
			},
			{
				Name:  "f3",
				Usage: "Check F3 service status",
				Action: func(c *cli.Context) error {
					return checkF3Running()
				},
			},
		},
	}
}

func chainCommands() *cli.Command {
	return &cli.Command{
		Name:  "chain",
		Usage: "Chain operations",
		Subcommands: []*cli.Command{
			{
				Name:  "backfill",
				Usage: "Check chain index backfill",
				Action: func(c *cli.Context) error {
					return performCheckBackfill(c.Context, config)
				},
			},
		},
	}
}

func stateCommands() *cli.Command {
	nodeFlag := &cli.StringFlag{
		Name:     "node",
		Usage:    "node name from config.json (Lotus1 or Lotus2)",
		Required: true,
	}

	return &cli.Command{
		Name:  "state",
		Usage: "State operations",
		Subcommands: []*cli.Command{
			{
				Name:  "check",
				Usage: "Check state consistency",
				Flags: []cli.Flag{
					nodeFlag,
				},
				Action: func(c *cli.Context) error {
					nodeConfig, err := getNodeConfig(c)
					if err != nil {
						return err
					}
					api, closer, err := resources.ConnectToNode(c.Context, *nodeConfig)
					if err != nil {
						return err
					}
					defer closer()
					return resources.StateMismatch(c.Context, api)
				},
			},
		},
	}
}

func stressCommands() *cli.Command {
	nodeFlag := &cli.StringFlag{
		Name:     "node",
		Usage:    "node name from config.json (Lotus1 or Lotus2)",
		Required: true,
	}

	return &cli.Command{
		Name:  "stress",
		Usage: "Stress test operations",
		Subcommands: []*cli.Command{
			{
				Name:  "messages",
				Usage: "Stress test with max size messages",
				Flags: []cli.Flag{
					nodeFlag,
				},
				Action: func(c *cli.Context) error {
					nodeConfig, err := getNodeConfig(c)
					if err != nil {
						return err
					}
					return performStressMaxMessageSize(c.Context, nodeConfig)
				},
			},
			{
				Name:  "p2p-bomb",
				Usage: "Run P2P bomb",
				Flags: []cli.Flag{
					nodeFlag,
				},
				Action: func(c *cli.Context) error {
					return resources.RunP2PBomb(c.Context, 100)
				},
			},
			{
				Name:  "block-fuzz",
				Usage: "Run block fuzzing",
				Flags: []cli.Flag{
					nodeFlag,
				},
				Action: func(c *cli.Context) error {
					nodeConfig, err := getNodeConfig(c)
					if err != nil {
						return err
					}
					return performBlockFuzzing(c.Context, nodeConfig)
				},
			},
		},
	}
}

func rpcCommands() *cli.Command {
	return &cli.Command{
		Name:  "rpc",
		Usage: "RPC operations",
		Subcommands: []*cli.Command{
			{
				Name:  "benchmark",
				Usage: "Run RPC benchmark tests",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "url",
						Usage:    "RPC endpoint URL",
						Required: true,
					},
				},
				Action: func(c *cli.Context) error {
					callV2API(c.String("url"))
					return nil
				},
			},
		},
	}
}

func ethCommands() *cli.Command {
	nodeFlag := &cli.StringFlag{
		Name:     "node",
		Usage:    "node name from config.json (Lotus1 or Lotus2)",
		Required: true,
	}

	return &cli.Command{
		Name:  "eth",
		Usage: "Ethereum compatibility operations",
		Subcommands: []*cli.Command{
			{
				Name:  "check",
				Usage: "Check ETH methods consistency",
				Action: func(c *cli.Context) error {
					return performEthMethodsCheck(c.Context)
				},
			},
			{
				Name:  "legacy-tx",
				Usage: "Send legacy Ethereum transaction",
				Flags: []cli.Flag{
					nodeFlag,
				},
				Action: func(c *cli.Context) error {
					nodeConfig, err := getNodeConfig(c)
					if err != nil {
						return err
					}
					return sendEthLegacyTransaction(c.Context, nodeConfig)
				},
			},
		},
	}
}

func performCreateOperation(ctx context.Context, nodeConfig *resources.NodeConfig, numWallets int, tokenAmount abi.TokenAmount) error {
	log.Printf("Creating %d wallets on node '%s'...", numWallets, nodeConfig.Name)

	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return nil
	}
	defer closer()

	err = resources.InitializeWallets(ctx, api, numWallets, tokenAmount)
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
	filteredNodes := resources.FilterLotusNodes(config.Nodes)
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
	if bytes.Equal(result, inputArgument) {
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

func performMempoolFuzz(ctx context.Context, nodeConfig *resources.NodeConfig, count, concurrency int, strategy string) error {
	log.Printf("[INFO] Starting mempool fuzzing on node '%s' with %d transactions using strategy '%s'...", nodeConfig.Name, count, strategy)

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
		if err := performCreateOperation(ctx, nodeConfig, numWallets, types.FromFil(100)); err != nil {
			log.Printf("Create operation failed: %v", err)
		}

		wallets, err = resources.GetAllWalletAddressesExceptGenesis(ctx, api)
		if err != nil || len(wallets) < 2 {
			return fmt.Errorf("failed to get enough wallet addresses after creation attempt: %v", err)
		}
	}

	from := wallets[0]
	to := wallets[1]

	// Call the appropriate fuzzing strategy
	return mpoolfuzz.FuzzMempoolWithStrategy(ctx, api, from, to, strategy, count)
}

func performMempoolTracking(ctx context.Context, nodeConfig *resources.NodeConfig, duration, interval time.Duration) error {
	log.Printf("[INFO] Starting mempool tracking on node '%s' for %v with %v intervals...", nodeConfig.Name, duration, interval)

	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return err
	}
	defer closer()

	// Create tracker with custom interval
	tracker := resources.NewMempoolTracker(api, interval)
	tracker.Start()

	// Wait for the specified duration
	select {
	case <-ctx.Done():
		tracker.Stop()
		return ctx.Err()
	case <-time.After(duration):
		tracker.Stop()
	}

	// Get final statistics
	stats := tracker.GetStats()
	log.Printf("[INFO] Mempool tracking completed on node '%s':", nodeConfig.Name)
	log.Printf("[INFO]   Total measurements: %v", stats["count"])
	log.Printf("[INFO]   Average size: %.2f", stats["average_size"])
	log.Printf("[INFO]   Min size: %v", stats["min_size"])
	log.Printf("[INFO]   Max size: %v", stats["max_size"])
	log.Printf("[INFO]   Data points: %v", stats["data_points"])

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
	filteredNodes := resources.FilterLotusNodes(config.Nodes)

	if len(filteredNodes) == 0 {
		return fmt.Errorf("no Lotus nodes found in config")
	}

	err := resources.CheckChainBackfill(ctx, filteredNodes)
	if err != nil {
		return fmt.Errorf("chain backfill check failed: %w", err)
	}
	assert.Sometimes(true, "Chain index backfill check completed.", map[string]interface{}{"requirement": "Chain index backfill check completed."})
	log.Println("[INFO] Chain index backfill check completed.")
	return nil
}

func performStressMaxMessageSize(ctx context.Context, nodeConfig *resources.NodeConfig) error {
	log.Printf("[INFO] Starting max message size stress test on node '%s'...", nodeConfig.Name)

	api, closer, err := resources.ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to Lotus node '%s': %w", nodeConfig.Name, err)
	}
	defer closer()

	err = resources.SendMaxSizedMessage(ctx, api)
	if err != nil {
		return fmt.Errorf("max message size stress test failed: %w", err)
	}

	log.Printf("[INFO] Max message size stress test completed successfully")
	return nil
}

func performCheckFinalizedTipsets(ctx context.Context) error {
	log.Printf("[INFO] Starting finalized tipset comparison...")

	// Load configuration
	config, err := resources.LoadConfig("/opt/antithesis/resources/config.json")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Filter nodes to get V1 and V2 nodes separately
	v1Nodes := resources.FilterLotusNodesV1(config.Nodes)
	v2Nodes := resources.FilterLotusNodesWithV2(config.Nodes)

	if len(v1Nodes) < 2 {
		return fmt.Errorf("need at least two Lotus V1 nodes for this test, found %d", len(v1Nodes))
	}
	if len(v2Nodes) < 2 {
		return fmt.Errorf("need at least two Lotus V2 nodes for this test, found %d", len(v2Nodes))
	}

	// Connect to V1 nodes to get chain heads and find common height range
	api1, closer1, err := resources.ConnectToNode(ctx, v1Nodes[0])
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", v1Nodes[0].Name, err)
	}
	defer closer1()

	api2, closer2, err := resources.ConnectToNode(ctx, v1Nodes[1])
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", v1Nodes[1].Name, err)
	}
	defer closer2()

	ch1, err := api1.ChainHead(ctx)
	if err != nil {
		return fmt.Errorf("failed to get chain head from %s: %w", v1Nodes[0].Name, err)
	}

	ch2, err := api2.ChainHead(ctx)
	if err != nil {
		return fmt.Errorf("failed to get chain head from %s: %w", v1Nodes[1].Name, err)
	}

	h1 := ch1.Height()
	h2 := ch2.Height()

	log.Printf("[INFO] Current height %d for node %s", h1, v1Nodes[0].Name)
	log.Printf("[INFO] Current height %d for node %s", h2, v1Nodes[1].Name)

	// Find the common height between both nodes
	var commonHeight int64
	if h1 < h2 {
		commonHeight = int64(h1)
	} else {
		commonHeight = int64(h2)
	}

	// Ensure we have enough history for F3 finalized tipset comparison
	// F3 starts from epoch 20, so we need at least 30 epochs to have a meaningful range
	if commonHeight < 30 {
		log.Printf("[WARN] chain height too low for finalized tipset comparison (common: %d, required: 30 for F3 range)", commonHeight)
		return nil
	}

	// Select a random height within the F3 range
	// F3 starts from epoch 20, and we leave 10 epochs buffer from the tip
	rand.Seed(time.Now().UnixNano())
	maxHeight := commonHeight - 10 // Leave 10 epochs buffer from tip
	minHeight := int64(20)         // F3 starts from epoch 20

	if maxHeight <= minHeight {
		log.Printf("[WARN] Not enough height range for finalized tipset comparison (min: %d, max: %d)", minHeight, maxHeight)
		return nil
	}

	randomHeight := minHeight + rand.Int63n(maxHeight-minHeight+1)
	log.Printf("[INFO] Selected height %d for finalized tipset comparison (range: %d-%d)", randomHeight, minHeight, maxHeight)

	// Connect to V2 nodes for finalized tipset comparison
	api11, closer11, err := resources.ConnectToNodeV2(ctx, v2Nodes[0])
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", v2Nodes[0].Name, err)
	}
	defer closer11()

	api22, closer22, err := resources.ConnectToNodeV2(ctx, v2Nodes[1])
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", v2Nodes[1].Name, err)
	}
	defer closer22()

	// Chain walk: Check 10 tipsets down from the selected height
	log.Printf("[INFO] Starting chain walk from height %d down to %d", randomHeight, randomHeight-9)

	for i := randomHeight; i >= randomHeight-9; i-- {
		log.Printf("[INFO] Checking finalized tipset at height %d", i)
		heightSelector := types.TipSetSelectors.Height(abi.ChainEpoch(i), true, types.TipSetAnchors.Finalized)

		ts1, err := api11.ChainGetTipSet(ctx, heightSelector)
		if err != nil {
			log.Printf("failed to get finalized tipset by height from %s: %w", v2Nodes[0].Name, err)
			return nil
		}
		log.Printf("[INFO] Finalized tipset %s on %s at height %d", ts1.Cids(), v2Nodes[0].Name, i)

		ts2, err := api22.ChainGetTipSet(ctx, heightSelector)
		if err != nil {
			log.Printf("failed to get finalized tipset by height from %s: %w", v2Nodes[1].Name, err)
			return nil
		}
		log.Printf("[INFO] Finalized tipset %s on %s at height %d", ts2.Cids(), v2Nodes[1].Name, i)

		assert.Always(ts1.Equals(ts2), "Chain synchronization test: Finalized tipset should always match",
			map[string]interface{}{
				"requirement": "Chain synchronization",
				"ts1":         ts1.Cids(),
				"ts2":         ts2.Cids(),
			})

		log.Printf("[INFO] Finalized tipsets %s match successfully at height %d", ts1.Cids(), i)
	}

	log.Printf("[INFO] Chain walk completed successfully - all 10 finalized tipsets match between nodes")
	return nil
}

func checkF3Running() error {
	urls := []string{
		"http://forest:23456",
		"http://lotus-1:1234",
		"http://lotus-2:1235",
	}

	request := `{"jsonrpc":"2.0","method":"Filecoin.F3IsRunning","params":[],"id":1}`

	for _, url := range urls {
		_, resp := resources.DoRawRPCRequest(url, 1, request)
		var response struct {
			Result bool `json:"result"`
		}
		if err := json.Unmarshal(resp, &response); err != nil {
			log.Printf("[WARN] Failed to parse response from %s: %v", url, err)
			continue
		}

		log.Printf("[INFO] F3 is running on %s: %v", url, response.Result)
		assert.Sometimes(response.Result, fmt.Sprintf("F3 is running on %s", url),
			map[string]interface{}{"requirement": fmt.Sprintf("F3 is running on %s", url)})
	}
	return nil
}

func checkPeers() error {
	urls := []string{
		"http://forest:3456",
		"http://lotus-1:1234",
		"http://lotus-2:1235",
	}

	request := `{"jsonrpc":"2.0","method":"Filecoin.NetPeers","params":[],"id":1}`

	disconnectedNodes := []string{}
	for _, url := range urls {
		_, resp := resources.DoRawRPCRequest(url, 1, request)
		var response struct {
			Result []struct {
				ID string `json:"ID"`
			} `json:"result"`
		}
		if err := json.Unmarshal(resp, &response); err != nil {
			log.Printf("[WARN] Failed to parse response from %s: %v", url, err)
			disconnectedNodes = append(disconnectedNodes, url)
			continue
		}

		peerCount := len(response.Result)
		if peerCount < 1 {
			disconnectedNodes = append(disconnectedNodes, url)
		}
		log.Printf("[INFO] Node %s has %d peers", url, peerCount)
	}

	if len(disconnectedNodes) > 0 {
		log.Printf("[WARN] The following nodes have less than 1 peer: %v", disconnectedNodes)
		assert.Sometimes(false, "All nodes should have at least 1 peer",
			map[string]interface{}{
				"requirement":        "Minimum 1 peer required",
				"disconnected_nodes": disconnectedNodes,
			})
	} else {
		log.Printf("[INFO] All nodes have at least 1 peer")
		assert.Sometimes(true, "All nodes have at least 1 peer",
			map[string]interface{}{
				"requirement": "Minimum 1 peer required",
			})
	}
	return nil
}
