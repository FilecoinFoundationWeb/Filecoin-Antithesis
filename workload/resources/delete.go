package resources

import (
	"context"
	"flag"
	"log"
	"math/rand"
	"time"
)

func delete() {
	ctx := context.Background()

	configFile := flag.String("config", "resources/config.json", "Path to config JSON file")
	nodeName := flag.String("node", "", "Name of the node to use from config.json")

	flag.Parse()

	// Load configuration
	config, err := LoadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Find the node configuration by name
	var nodeConfig *NodeConfig
	for _, node := range config.Nodes {
		if node.Name == *nodeName {
			nodeConfig = &node
			break
		}
	}
	if nodeConfig == nil {
		log.Fatalf("Node with name '%s' not found in config.json", *nodeName)
	}

	// Connect to the chosen node
	api, closer, err := ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Fatalf("Failed to connect to Lotus node: %v", err)
	}
	defer closer()

	// Get all wallets except the genesis wallet
	allWallets, err := GetAllWalletAddressesExceptGenesis(ctx, api)
	if err != nil {
		log.Fatalf("Failed to get all wallets: %v", err)
	}

	// Determine a random number of wallets to delete
	rand.Seed(time.Now().UnixNano())
	numWallets := rand.Intn(len(allWallets)) + 1 // Random number between 1 and len(allWallets)

	// Select random wallets to delete
	walletsToDelete := allWallets[:numWallets]

	if len(walletsToDelete) == 0 {
		log.Println("No wallets available for deletion.")
		return
	}

	// Delete the selected wallets
	err = DeleteWallets(ctx, api, walletsToDelete)
	if err != nil {
		log.Fatalf("Failed to delete wallets: %v", err)
	}

	log.Printf("Deleted %d wallets successfully on node '%s'.", len(walletsToDelete), *nodeName)
}
