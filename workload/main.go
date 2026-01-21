package main

import (
	"log"
	"os"
	"strings"
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
			minerCommands(),
			stressCommands(),
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

					// Forest specific handling
					if nodeConfig.Name == "Forest" || strings.HasPrefix(nodeConfig.Name, "Forest") {
						api, closer, err := resources.ConnectToNode(c.Context, *nodeConfig)
						if err != nil {
							return err
						}
						defer closer()

						// Find a Lotus node to fund from
						lotusNodes := resources.FilterLotusNodes(config.Nodes)
						if len(lotusNodes) == 0 {
							log.Printf("[ERROR] No Lotus nodes available to fund Forest wallets")
							return nil
						}

						// Pick first Lotus node
						lotusNodeConfig := lotusNodes[0]
						lotusApi, lotusCloser, err := resources.ConnectToNode(c.Context, lotusNodeConfig)
						if err != nil {
							log.Printf("[ERROR] Failed to connect to Lotus node %s for funding: %v", lotusNodeConfig.Name, err)
							return err
						}
						defer lotusCloser()

						return resources.CreateForestWallets(c.Context, api, lotusApi, c.Int("count"), abi.NewTokenAmount(1000000000000))
					}

					// Standard Lotus wallet creation
					return resources.PerformCreateOperation(c.Context, nodeConfig, c.Int("count"), abi.NewTokenAmount(1000000000000))
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
						log.Printf("[ERROR] Failed to connect to node '%s': %v", nodeConfig.Name, err)
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
						log.Printf("[ERROR] Failed to connect to node '%s': %v", nodeConfig.Name, err)
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
				},
				Action: func(c *cli.Context) error {
					nodeConfig, err := getNodeConfig(c)
					if err != nil {
						return err
					}
					return resources.PerformReorgOperation(c.Context, nodeConfig, false)
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
	return &cli.Command{
		Name:        "contracts",
		Usage:       "Smart contract operations (Deprecated)",
		Subcommands: []*cli.Command{},
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
		},
	}
}

func monitoringCommands() *cli.Command {
	return &cli.Command{
		Name:  "monitor",
		Usage: "Monitoring operations",
		Subcommands: []*cli.Command{
			{
				Name:  "height-progression",
				Usage: "Monitor height progression for all nodes",
				Flags: []cli.Flag{
					&cli.DurationFlag{
						Name:  "duration",
						Usage: "Duration to monitor (e.g., 30s, 1m, 2m)",
						Value: 30 * time.Second,
					},
					&cli.DurationFlag{
						Name:  "interval",
						Usage: "Interval between height checks (e.g., 5s, 7s, 10s)",
						Value: 7 * time.Second,
					},
					&cli.IntFlag{
						Name:  "max-stalls",
						Usage: "Maximum consecutive stalls before failing",
						Value: 10,
					},
				},
				Action: func(c *cli.Context) error {
					monitorConfig := &resources.HealthMonitorConfig{
						EnableChainNotify:       false,
						EnableHeightProgression: true,
						EnablePeerCount:         false,
						EnableF3Status:          false,
						MonitorDuration:         c.Duration("duration"),
						HeightCheckInterval:     c.Duration("interval"),
						MaxConsecutiveStalls:    c.Int("max-stalls"),
					}
					return resources.ComprehensiveHealthCheckWithConfig(c.Context, config, monitorConfig)
				},
			},
			{
				Name:  "comprehensive",
				Usage: "Perform comprehensive health check with all monitoring features",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "chain-notify",
						Usage: "Enable chain notify monitoring (tipset changes)",
						Value: true,
					},
					&cli.BoolFlag{
						Name:  "height-progression",
						Usage: "Enable height progression monitoring",
						Value: true,
					},

					&cli.BoolFlag{
						Name:  "peer-count",
						Usage: "Enable peer count monitoring",
						Value: true,
					},
					&cli.BoolFlag{
						Name:  "f3-status",
						Usage: "Enable F3 running status checks",
						Value: true,
					},
					&cli.DurationFlag{
						Name:  "monitor-duration",
						Usage: "Duration to monitor for chain notify and height progression (e.g., 30s, 1m, 2m)",
						Value: 30 * time.Second,
					},
					&cli.DurationFlag{
						Name:  "height-check-interval",
						Usage: "Interval between height checks (e.g., 5s, 7s, 10s)",
						Value: 7 * time.Second,
					},
					&cli.IntFlag{
						Name:  "max-consecutive-stalls",
						Usage: "Maximum consecutive stalls before failing height progression check",
						Value: 10,
					},
				},
				Action: func(c *cli.Context) error {
					// Create a custom config for the health monitor
					monitorConfig := &resources.HealthMonitorConfig{
						EnableChainNotify:       c.Bool("chain-notify"),
						EnableHeightProgression: c.Bool("height-progression"),
						EnablePeerCount:         c.Bool("peer-count"),
						EnableF3Status:          c.Bool("f3-status"),
						MonitorDuration:         c.Duration("monitor-duration"),
						HeightCheckInterval:     c.Duration("height-check-interval"),
						MaxConsecutiveStalls:    c.Int("max-consecutive-stalls"),
					}

					return resources.ComprehensiveHealthCheckWithConfig(c.Context, config, monitorConfig)
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
			{
				Name:  "common-tipset",
				Usage: "Resolve and print the common finalized tipset across all nodes",
				Flags: []cli.Flag{
					&cli.DurationFlag{
						Name:  "timeout",
						Value: 2 * time.Minute,
						Usage: "Timeout for resolution",
					},
				},
				Action: func(c *cli.Context) error {
					// Connect to V1 nodes only (excludes *-V2 nodes), includes Forest
					v1Nodes := resources.FilterV1Nodes(config.Nodes)
					var commonAPIs []resources.CommonAPI
					for _, nc := range v1Nodes {
						api, closer, err := resources.ConnectToCommonNode(c.Context, nc)
						if err != nil {
							log.Printf("[ERROR] Failed to connect to %s: %v", nc.Name, err)
							continue
						}
						defer closer()
						commonAPIs = append(commonAPIs, api)
					}

					if len(commonAPIs) == 0 {
						log.Printf("[ERROR] No nodes connected")
						return nil
					}

					ts, err := resources.GetCommonTipSet(c.Context, commonAPIs)
					if err != nil {
						log.Printf("[ERROR] Failed to get common tipset: %v", err)
						return err
					}

					log.Printf("[SUCCESS] Common Finalized TipSet: Height %d, Key %s", ts.Height(), ts.Key())
					return nil
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
				Usage: "Check state consistency for a single node",
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
			{
				Name:  "compare",
				Usage: "Compare state across all nodes from common finalized tipset",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:  "epochs",
						Value: 10,
						Usage: "Number of epochs to walk back and compare",
					},
				},
				Action: func(c *cli.Context) error {
					log.Println("[INFO] Starting cross-node state comparison...")
					return resources.PerformCrossNodeStateCheck(c.Context, config)
				},
			},
			{
				Name:  "compare-at-height",
				Usage: "Compare state across all nodes at a specific height",
				Flags: []cli.Flag{
					&cli.Int64Flag{
						Name:     "height",
						Usage:    "Chain height to compare state at",
						Required: true,
					},
				},
				Action: func(c *cli.Context) error {
					height := abi.ChainEpoch(c.Int64("height"))
					log.Printf("[INFO] Comparing state across all nodes at height %d...", height)
					return resources.CompareStateAtHeight(c.Context, config.Nodes, height)
				},
			},
		},
	}
}

func minerCommands() *cli.Command {
	return &cli.Command{
		Name:        "miner",
		Usage:       "Miner operations (Deprecated)",
		Subcommands: []*cli.Command{},
	}
}

func stressCommands() *cli.Command {
	return &cli.Command{
		Name:        "stress",
		Usage:       "Stress test operations (Deprecated in favor of mempool)",
		Subcommands: []*cli.Command{},
	}
}

func ethCommands() *cli.Command {
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
		},
	}
}
