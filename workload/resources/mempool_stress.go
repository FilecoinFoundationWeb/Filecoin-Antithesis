package resources

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	mpoolfuzz "github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources/mpool-fuzz"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

// PerformMempoolFuzz runs mempool fuzzing on a specified node
func PerformMempoolFuzz(ctx context.Context, nodeConfig *NodeConfig, count, concurrency int, strategy string) error {
	log.Printf("[INFO] Starting mempool fuzzing on node '%s' with %d transactions using strategy '%s'...", nodeConfig.Name, count, strategy)

	api, closer, err := ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return nil
	}
	defer closer()

	return RetryOperation(ctx, func() error {
		wallets, err := GetAllWalletAddressesExceptGenesis(ctx, api)
		if err != nil {
			log.Printf("[WARN] Failed to get wallet addresses, will retry: %v", err)
			return err // Return original error for retry
		}

		if len(wallets) < 2 {
			log.Printf("[WARN] Not enough wallets (found %d). Creating more wallets.", len(wallets))
			numWallets := 2
			if err := PerformCreateOperation(ctx, nodeConfig, numWallets, types.FromFil(100)); err != nil {
				log.Printf("[WARN] Create operation failed, will retry: %v", err)
				return err // Return original error for retry
			}

			wallets, err = GetAllWalletAddressesExceptGenesis(ctx, api)
			if err != nil || len(wallets) < 2 {
				log.Printf("[WARN] Still not enough wallets after creation, will retry")
				return err // Return original error for retry
			}
		}

		from := wallets[0]
		to := wallets[1]

		// Call the appropriate fuzzing strategy
		return mpoolfuzz.FuzzMempoolWithStrategy(ctx, api, from, to, strategy, count)
	}, "Mempool fuzzing operation")
}

// PerformMempoolTracking tracks mempool size over time
func PerformMempoolTracking(ctx context.Context, nodeConfig *NodeConfig, duration, interval time.Duration) error {
	log.Printf("[INFO] Starting mempool tracking on node '%s' for %v with %v intervals...", nodeConfig.Name, duration, interval)

	api, closer, err := ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return nil
	}
	defer closer()

	return RetryOperation(ctx, func() error {
		// Create tracker with custom interval
		tracker := NewMempoolTracker(api, interval)
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
	}, "Mempool tracking operation")
}

// PerformSpamOperation sends valid spam transactions between wallets on all nodes
func PerformSpamOperation(ctx context.Context, config *Config) error {
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
	filteredNodes := FilterV1Nodes(config.Nodes)
	log.Printf("[INFO] Filtered nodes for spam operation: %+v", filteredNodes)

	// Connect to each node and retrieve wallets
	for _, node := range filteredNodes {
		log.Printf("[INFO] Connecting to Lotus node '%s'...", node.Name)
		api, closer, err := ConnectToNode(ctx, node)
		if err != nil {
			log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", node.Name, err)
			return nil
		}
		closers = append(closers, closer)

		// Use RetryOperation for wallet operations on each node
		err = RetryOperation(ctx, func() error {
			// Ensure wallets have sufficient funds before proceeding
			log.Printf("[INFO] Checking wallet funds for node '%s'...", node.Name)
			_, err := GetAllWalletAddressesExceptGenesis(ctx, api)
			if err != nil {
				log.Printf("[WARN] Failed to ensure wallets are funded on '%s': %v", node.Name, err)
				// Create some wallets if needed
				numWallets := 3
				log.Printf("[INFO] Creating %d new wallets on node '%s'...", numWallets, node.Name)

				// Check if this is a Forest node
				if node.Name == "Forest" {
					// For Forest nodes, we need the Lotus API to fund from genesis
					lotusNode := NodeConfig{
						Name:          "Lotus1",
						RPCURL:        "http://lotus0:1234/rpc/v1",
						AuthTokenPath: "/root/devgen/lotus-1/jwt",
					}
					lotusApi, lotusCloser, err := ConnectToNode(ctx, lotusNode)
					if err != nil {
						log.Printf("[ERROR] Failed to connect to Lotus node for Forest wallet initialization: %v", err)
						return nil
					}
					defer lotusCloser()

					if err := InitializeForestWallets(ctx, api, lotusApi, numWallets, abi.NewTokenAmount(1000000000000000)); err != nil {
						log.Printf("[ERROR] Failed to create new Forest wallets: %v", err)
						return nil
					}
				} else {
					// For Lotus nodes, use standard wallet initialization
					if err := InitializeWallets(ctx, api, numWallets, abi.NewTokenAmount(1000000000000000)); err != nil {
						log.Printf("[ERROR] Failed to create new wallets: %v", err)
						return nil
					}
				}
			}

			log.Printf("[INFO] Retrieving wallets for node '%s'...", node.Name)
			nodeWallets, err := GetAllWalletAddressesExceptGenesis(ctx, api)
			if err != nil {
				log.Printf("[ERROR] Failed to retrieve wallets for node '%s': %v", node.Name, err)
				return nil
			}
			log.Printf("[INFO] Retrieved %d wallets for node '%s'.", len(nodeWallets), node.Name)

			if len(nodeWallets) < 2 {
				log.Printf("[ERROR] Not enough wallets on node '%s' (found %d). At least 2 needed for spam operation",
					node.Name, len(nodeWallets))
			}

			apis = append(apis, api)
			wallets = append(wallets, nodeWallets)
			return nil
		}, fmt.Sprintf("Wallet setup for node %s", node.Name))

		if err != nil {
			log.Printf("[WARN] Failed to setup wallets for node '%s': %v", node.Name, err)
			continue
		}
	}

	// Ensure we have enough nodes connected for spam
	if len(apis) < 1 {
		log.Printf("[ERROR] Not enough nodes available for spam operation")
		return nil
	}

	// Use RetryOperation for the spam transactions
	return RetryOperation(ctx, func() error {
		// Perform spam transactions
		rand.Seed(time.Now().UnixNano())
		numTransactions := rand.Intn(30) + 1
		log.Printf("[INFO] Initiating spam operation with %d transactions...", numTransactions)
		if err := SpamTransactions(ctx, apis, wallets, numTransactions); err != nil {
			log.Printf("[ERROR] Spam operation failed: %v", err)
			return nil
		}
		log.Println("[INFO] Spam operation completed successfully.")
		return nil
	}, "Spam transactions operation")
}
