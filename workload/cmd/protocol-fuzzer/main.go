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
	targets       []TargetNode
	lotusTargets  []TargetNode
	forestTargets []TargetNode

	// Network metadata
	networkName string
	genesisCID  string

	// Identity pool for ephemeral libp2p hosts
	pool *IdentityPool

	// Weighted attack deck
	deck []namedAttack
)

// nodeType controls which node types an attack targets.
type nodeType int

const (
	nodeAny    nodeType = 0 // send to any node
	nodeLotus  nodeType = 1 // Lotus-specific (Go)
	nodeForest nodeType = 2 // Forest-specific (Rust + Go F3 sidecar)
)

// namedAttack pairs an attack function with its name and target type.
type namedAttack struct {
	name       string
	fn         func()           // legacy: picks own target (GossipSub broadcasts)
	targetedFn func(TargetNode) // new: receives pre-selected target
	targetType nodeType
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

	// Split targets by node type for targeted attacks
	for _, t := range targets {
		if strings.HasPrefix(t.Name, "forest") {
			forestTargets = append(forestTargets, t)
		} else {
			lotusTargets = append(lotusTargets, t)
		}
	}
	log.Printf("[protocol-fuzzer] lotus=%d forest=%d", len(lotusTargets), len(forestTargets))

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

	// Create gossip publisher (reuses host across publishes)
	gossipPub = newGossipPublisher(50)
	defer gossipPub.close()

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

		if attack.targetedFn != nil {
			target := pickTargetForType(attack.targetType)
			if target == nil {
				continue // no suitable target for this attack type
			}
			log.Printf("[protocol-fuzzer] starting vector=%s target=%s", attack.name, target.Name)
			attack.targetedFn(*target)
		} else {
			log.Printf("[protocol-fuzzer] starting vector=%s", attack.name)
			attack.fn()
		}
		log.Printf("[protocol-fuzzer] completed vector=%s", attack.name)

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

// pickTargetForType returns a random target matching the requested node type.
// Returns nil if no matching target is available.
func pickTargetForType(nt nodeType) *TargetNode {
	var pool []TargetNode
	switch nt {
	case nodeLotus:
		pool = lotusTargets
	case nodeForest:
		pool = forestTargets
	default:
		pool = targets
	}
	if len(pool) == 0 {
		return nil
	}
	t := rngChoice(pool)
	return &t
}

func buildDeck() {
	type weightedCategory struct {
		envVar    string
		defWeight int
		attacks   []namedAttack
	}

	categories := []weightedCategory{
		{"FUZZER_WEIGHT_CHAINEXCHANGE_RESPONSES", 3, getAllExchangeServerAttacks()},
		{"FUZZER_WEIGHT_BLOCK_AND_MESSAGE_VALIDATION", 3, getAllGossipAttacks()},
		{"FUZZER_WEIGHT_LIBP2P_CONNECTION_ABUSE", 2, getAllChaosAttacks()},
		{"FUZZER_WEIGHT_CBOR_DECODER_STRESS", 4, getAllCBORBombAttacks()},
		{"FUZZER_WEIGHT_F3_GRANITE_CONSENSUS", 4, getAllF3Attacks()},
		{"FUZZER_WEIGHT_F3_CHAIN_EXCHANGE", 4, getAllF3ChainExAttacks()},
		{"FUZZER_WEIGHT_F3_CERT_EXCHANGE", 3, getAllF3CertExAttacks()},
		{"FUZZER_WEIGHT_HELLO_PROTOCOL", 3, getAllHelloAttacks()},
		{"FUZZER_WEIGHT_RUST_SPECIFIC_ATTACKS", 3, getAllForestAttacks()},
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
