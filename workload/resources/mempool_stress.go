package resources

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
)

const (
	// MinWalletsForSpam is the minimum number of wallets needed per node for spam
	MinWalletsForSpam = 2
	// DefaultSpamWalletFunding is the funding amount for new spam wallets (1 FIL)
	DefaultSpamWalletFunding = 1000000000000000000
)

// PerformMempoolTracking tracks mempool size over time
func PerformMempoolTracking(ctx context.Context, nodeConfig *NodeConfig, duration, interval time.Duration) error {
	log.Printf("[INFO] Starting mempool tracking on node '%s' for %v with %v intervals...", nodeConfig.Name, duration, interval)

	api, closer, err := ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to node '%s': %w", nodeConfig.Name, err)
	}
	defer closer()

	return RetryOperation(ctx, func() error {
		tracker := NewMempoolTracker(api, interval)
		tracker.Start()

		select {
		case <-ctx.Done():
			tracker.Stop()
			return ctx.Err()
		case <-time.After(duration):
			tracker.Stop()
		}

		stats := tracker.GetStats()
		log.Printf("[INFO] Mempool tracking completed on node '%s':", nodeConfig.Name)
		log.Printf("[INFO]   Total measurements: %v", stats["count"])
		log.Printf("[INFO]   Average size: %.2f", stats["average_size"])
		log.Printf("[INFO]   Min size: %v", stats["min_size"])
		log.Printf("[INFO]   Max size: %v", stats["max_size"])

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

	filteredNodes := FilterV1Nodes(config.Nodes)
	log.Printf("[INFO] Filtered nodes for spam operation: %d nodes", len(filteredNodes))

	// Connect to each node and ensure wallets exist
	for _, node := range filteredNodes {
		log.Printf("[INFO] Connecting to node '%s'...", node.Name)
		nodeApi, closer, err := ConnectToNode(ctx, node)
		if err != nil {
			log.Printf("[ERROR] Failed to connect to node '%s': %v", node.Name, err)
			continue
		}
		closers = append(closers, closer)

		// Get existing wallets
		nodeWallets, err := GetAllWalletAddressesExceptGenesis(ctx, nodeApi)
		if err != nil {
			log.Printf("[WARN] Failed to get wallets for '%s': %v", node.Name, err)
			nodeWallets = []address.Address{}
		}

		log.Printf("[INFO] Node '%s' has %d wallets", node.Name, len(nodeWallets))

		// Auto-create wallets if needed
		if len(nodeWallets) < MinWalletsForSpam {
			walletsNeeded := MinWalletsForSpam - len(nodeWallets)
			log.Printf("[INFO] Creating %d wallets on node '%s'...", walletsNeeded, node.Name)

			if err := ensureWalletsOnNode(ctx, nodeApi, node, filteredNodes, walletsNeeded); err != nil {
				log.Printf("[WARN] Failed to create wallets on '%s': %v", node.Name, err)
			}

			// Refresh wallet list
			nodeWallets, _ = GetAllWalletAddressesExceptGenesis(ctx, nodeApi)
			log.Printf("[INFO] Node '%s' now has %d wallets", node.Name, len(nodeWallets))
		}

		apis = append(apis, nodeApi)
		wallets = append(wallets, nodeWallets)
	}

	if len(apis) < 1 {
		return fmt.Errorf("no nodes available for spam operation")
	}

	// Check we have at least one node with wallets
	hasWallets := false
	for _, w := range wallets {
		if len(w) > 0 {
			hasWallets = true
			break
		}
	}
	if !hasWallets {
		return fmt.Errorf("no wallets available on any node")
	}

	return RetryOperation(ctx, func() error {
		rand.Seed(time.Now().UnixNano())
		numTransactions := rand.Intn(30) + 1
		log.Printf("[INFO] Initiating spam operation with %d transactions...", numTransactions)

		if err := SpamTransactions(ctx, apis, wallets, numTransactions); err != nil {
			log.Printf("[ERROR] Spam operation failed: %v", err)
			return err
		}

		log.Println("[INFO] Spam operation completed successfully.")
		return nil
	}, "Spam transactions operation")
}

// ensureWalletsOnNode creates and funds wallets on a node
func ensureWalletsOnNode(ctx context.Context, nodeApi api.FullNode, node NodeConfig, allNodes []NodeConfig, count int) error {
	fundingAmount := abi.NewTokenAmount(DefaultSpamWalletFunding)

	// Check if this is a Forest node
	if strings.HasPrefix(node.Name, "Forest") {
		// For Forest, we need to fund from a Lotus node
		lotusNodes := FilterLotusNodes(allNodes)
		if len(lotusNodes) == 0 {
			return fmt.Errorf("no Lotus nodes available to fund Forest wallets")
		}

		lotusApi, lotusCloser, err := ConnectToNode(ctx, lotusNodes[0])
		if err != nil {
			return fmt.Errorf("failed to connect to Lotus for funding: %w", err)
		}
		defer lotusCloser()

		return CreateForestWallets(ctx, nodeApi, lotusApi, count, fundingAmount)
	}

	// For Lotus nodes, use standard initialization
	return InitializeWallets(ctx, nodeApi, count, fundingAmount)
}
