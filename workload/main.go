package main

import (
	"log"
	"os"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/filecoin-project/go-state-types/abi"
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
				log.Printf("[ERROR] Failed to load config: %v", err)
				return nil
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
		log.Printf("[ERROR] Node name is required")
		return nil, nil
	}

	for i := range config.Nodes {
		if config.Nodes[i].Name == nodeName {
			return &config.Nodes[i], nil
		}
	}
	log.Printf("[ERROR] Node '%s' not found in config", nodeName)
	return nil, nil
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
					if nodeConfig.Name == "Forest" {
						api, closer, err := resources.ConnectToNode(c.Context, *nodeConfig)
						if err != nil {
							return err
						}
						defer closer()
						return resources.CreateForestWallets(c.Context, api, c.Int("count"), abi.NewTokenAmount(10000000000000))
					} else {
						return resources.PerformCreateOperation(c.Context, nodeConfig, c.Int("count"), abi.NewTokenAmount(1000000000000000))
					}
				},
			},
			{
				Name:  "fund",
				Usage: "Fund forest genesis wallet",
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
					lotusNodeConfig := resources.NodeConfig{
						Name:          "Lotus1",
						RPCURL:        "http://lotus-1:1234/rpc/v1",
						AuthTokenPath: "/root/devgen/lotus-1/jwt",
					}
					lotusapi, closer, err := resources.ConnectToNode(c.Context, lotusNodeConfig)
					if err != nil {
						return err
					}
					defer closer()
					return resources.InitializeForestWallets(c.Context, api, lotusapi, 1, abi.NewTokenAmount(1000000000000000000))
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
					return resources.PerformDeleteOperation(c.Context, nodeConfig)
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
						log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
						return nil
					}
					defer closer()

					lotusNodes := resources.FilterV1Nodes(config.Nodes)
					if err := resources.ConnectToOtherNodes(c.Context, api, *nodeConfig, lotusNodes); err != nil {
						log.Printf("[ERROR] Failed to connect node '%s' to other nodes: %v", nodeConfig.Name, err)
						return nil
					}
					log.Printf("Node '%s' connected successfully", nodeConfig.Name)
					return nil
				},
			},
			{
				Name:  "notify",
				Usage: "Poll the chain for new updates",
				Action: func(c *cli.Context) error {
					return resources.PerformNotifyOperation(c.Context, config)
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
						log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
						return nil
					}
					defer closer()

					if err := resources.DisconnectFromOtherNodes(c.Context, api); err != nil {
						log.Printf("[ERROR] Failed to disconnect node '%s' from other nodes: %v", nodeConfig.Name, err)
						return nil
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
					nodeConfig, err := getNodeConfig(c)
					if err != nil {
						return err
					}
					return resources.PerformReorgOperation(c.Context, nodeConfig, c.Bool("check-consensus"))
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
					return resources.PerformMempoolFuzz(
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
					return resources.PerformMempoolFuzz(
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
					return resources.PerformMempoolTracking(
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
					return resources.PerformSpamOperation(c.Context, config)
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
					return resources.PerformDeploySimpleCoin(c.Context, nodeConfig, simpleCoinPath)
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
					return resources.PerformDeployMCopy(c.Context, nodeConfig, mcopyPath)
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
					return resources.PerformDeployTStore(c.Context, nodeConfig, tstoragePath)
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
					filteredConfig := resources.FilterV1Nodes(config.Nodes)
					return resources.PerformConsensusCheck(c.Context, &resources.Config{Nodes: filteredConfig}, c.Int64("height"))
				},
			},
			{
				Name:  "fault",
				Usage: "Send consensus fault",
				Action: func(c *cli.Context) error {
					return resources.PerformSendConsensusFault(c.Context)
				},
			},
			{
				Name:  "finalized",
				Usage: "Check finalized tipsets",
				Action: func(c *cli.Context) error {
					return resources.PerformCheckFinalizedTipsets(c.Context)
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
					return resources.CheckPeers()
				},
			},
			{
				Name:  "f3",
				Usage: "Check F3 service status",
				Action: func(c *cli.Context) error {
					return resources.CheckF3Running()
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
					return resources.PerformCheckBackfill(c.Context, config)
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
					return resources.PerformStateCheck(c.Context, nodeConfig)
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
					return resources.PerformStressMaxMessageSize(c.Context, nodeConfig)
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
					return resources.PerformBlockFuzzing(c.Context, nodeConfig)
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
					resources.CallV2API(c.String("url"))
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
					return resources.PerformEthMethodsCheck(c.Context)
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
					return resources.SendEthLegacyTransaction(c.Context, nodeConfig)
				},
			},
		},
	}
}
