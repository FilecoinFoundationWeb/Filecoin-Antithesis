package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/lifecycle"
)

// ---------------------------------------------------------------------------
// Global State (flat architecture — matches stress-engine pattern)
// ---------------------------------------------------------------------------

var (
	ctx    context.Context
	cancel context.CancelFunc

	// Discovered libp2p targets
	targets []TargetNode

	// Network metadata
	networkName string
	genesisCID  string

	// Identity pool for ephemeral libp2p hosts
	pool *IdentityPool

	// Weighted attack deck
	deck []namedAttack
)

// namedAttack pairs an attack function with its name for logging.
type namedAttack struct {
	name string
	fn   func()
}

// ---------------------------------------------------------------------------
// Initialization
// ---------------------------------------------------------------------------

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("[protocol-fuzzer] protocol fuzzer starting")

	if envOrDefault("FUZZER_ENABLED", "1") != "1" {
		log.Println("[protocol-fuzzer] disabled via FUZZER_ENABLED=0, exiting")
		return
	}

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	// Parse node names from env (same var as stress-engine)
	nodeNames := strings.Split(envOrDefault("STRESS_NODES", "lotus0"), ",")
	devgenDir := envOrDefault("FUZZER_DEVGEN_DIR", "/root/devgen")

	// Discover libp2p peers
	log.Println("[protocol-fuzzer] discovering libp2p peers...")
	targets = waitForNodes(nodeNames, devgenDir)
	log.Printf("[protocol-fuzzer] discovered %d targets", len(targets))

	// Network name — always "2k" for devnet
	networkName = "2k"

	// Discover genesis CID via RPC
	rpcPort := envOrDefault("STRESS_RPC_PORT", "1234")
	rpcURL := fmt.Sprintf("http://lotus0:%s/rpc/v1", rpcPort)
	genesisCID = discoverGenesisCID(rpcURL)

	// Create identity pool
	poolSize := envInt("FUZZER_IDENTITY_POOL_SIZE", 20)
	pool = newIdentityPool(poolSize)
	defer pool.CloseAll()

	// Build weighted attack deck
	buildDeck()

	lifecycle.SetupComplete(map[string]any{
		"targets":      len(targets),
		"network_name": networkName,
		"genesis_cid":  genesisCID,
		"deck_size":    len(deck),
	})

	log.Println("[protocol-fuzzer] entering main loop")

	// Main attack loop
	interval := time.Duration(envInt("FUZZER_RATE_MS", 500)) * time.Millisecond
	actionCounts := make(map[string]int)
	iteration := 0

	for {
		attack := deck[rngIntn(len(deck))]
		target := rngChoice(targets)

		log.Printf("[protocol-fuzzer] starting vector=%s target=%s", attack.name, target.Name)
		attack.fn()
		log.Printf("[protocol-fuzzer] completed vector=%s target=%s", attack.name, target.Name)

		actionCounts[attack.name]++
		iteration++

		// Periodic summary every 100 iterations
		if iteration%100 == 0 {
			log.Printf("[protocol-fuzzer] === iteration %d summary ===", iteration)
			for name, count := range actionCounts {
				log.Printf("[protocol-fuzzer]   %s: %d", name, count)
			}
		}

		time.Sleep(interval)
	}
}

// ---------------------------------------------------------------------------
// Deck building (weighted, same pattern as stress-engine)
// ---------------------------------------------------------------------------

func buildDeck() {
	type weightedCategory struct {
		envVar    string
		defWeight int
		attacks   []namedAttack
	}

	categories := []weightedCategory{
		{"FUZZER_WEIGHT_EXCHANGE_SERVER", 3, getAllExchangeServerAttacks()},
		{"FUZZER_WEIGHT_GOSSIP", 3, getAllGossipAttacks()},
		{"FUZZER_WEIGHT_CHAOS", 2, getAllChaosAttacks()},
		{"FUZZER_WEIGHT_CBOR_BOMBS", 4, getAllCBORBombAttacks()},
		{"FUZZER_WEIGHT_F3", 4, getAllF3Attacks()},
	}

	deck = nil
	for _, cat := range categories {
		w := envInt(cat.envVar, cat.defWeight)
		if w <= 0 || len(cat.attacks) == 0 {
			continue
		}
		log.Printf("[protocol-fuzzer] category %s: weight=%d attacks=%d", cat.envVar, w, len(cat.attacks))
		for i := 0; i < w; i++ {
			deck = append(deck, cat.attacks...)
		}
	}

	if len(deck) == 0 {
		log.Fatal("[protocol-fuzzer] FATAL: deck is empty — set at least one FUZZER_WEIGHT_* > 0")
	}
	log.Printf("[protocol-fuzzer] deck built with %d entries", len(deck))
}
