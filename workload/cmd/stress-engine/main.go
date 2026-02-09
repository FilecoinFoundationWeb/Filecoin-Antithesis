package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"workload/internal/chain"

	"github.com/antithesishq/antithesis-sdk-go/lifecycle"
	"github.com/antithesishq/antithesis-sdk-go/random"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	_ "github.com/filecoin-project/lotus/lib/sigs/secp"
)

// ---------------------------------------------------------------------------
// Global State (flat architecture — no constructors, no DI)
// ---------------------------------------------------------------------------

var (
	ctx    context.Context
	cancel context.CancelFunc

	// Node connections: key = node hostname (e.g. "lotus0")
	nodes    map[string]api.FullNode
	nodeKeys []string

	// Wallet state loaded from stress_keystore.json
	keystore map[address.Address]*types.KeyInfo
	addrs    []address.Address

	// Per-address monotonic nonce counter
	nonces map[address.Address]uint64

	// Weighted action deck with names for logging
	deck []namedAction
)

// namedAction pairs an action function with its name for logging
type namedAction struct {
	name string
	fn   func()
}

// ---------------------------------------------------------------------------
// Configuration helpers
// ---------------------------------------------------------------------------

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Printf("[config] invalid int for %s=%q, using default %d", key, v, fallback)
		return fallback
	}
	return n
}

// ---------------------------------------------------------------------------
// Randomness helpers (Antithesis SDK — deterministic)
// ---------------------------------------------------------------------------

func rngIntn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(random.GetRandom() % uint64(n))
}

func rngChoice[T any](items []T) T {
	return random.RandomChoice(items)
}

func pickNode() (string, api.FullNode) {
	name := rngChoice(nodeKeys)
	return name, nodes[name]
}

func pickWallet() (address.Address, *types.KeyInfo) {
	addr := rngChoice(addrs)
	return addr, keystore[addr]
}

// ---------------------------------------------------------------------------
// Initialization
// ---------------------------------------------------------------------------

func connectNodes() {
	cfg := chain.NodeConfig{
		Names: strings.Split(envOrDefault("STRESS_NODES", "lotus0"), ","),
		Port:  envOrDefault("STRESS_RPC_PORT", "1234"),
	}

	var err error
	nodes, nodeKeys, err = chain.ConnectNodes(ctx, cfg)
	if err != nil {
		log.Fatalf("[init] FATAL: %v", err)
	}
}

// KeystoreEntry matches the JSON format written by genesis-prep.
type KeystoreEntry struct {
	Address    string `json:"Address"`
	PrivateKey string `json:"PrivateKey"`
}

func loadKeystore() {
	path := envOrDefault("STRESS_KEYSTORE_PATH", "/shared/stress_keystore.json")
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("[init] FATAL: cannot read keystore at %s: %v", path, err)
	}

	var entries []KeystoreEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		log.Fatalf("[init] FATAL: cannot parse keystore: %v", err)
	}

	keystore = make(map[address.Address]*types.KeyInfo, len(entries))
	nonces = make(map[address.Address]uint64, len(entries))
	addrs = make([]address.Address, 0, len(entries))

	for _, e := range entries {
		addr, err := address.NewFromString(e.Address)
		if err != nil {
			log.Printf("[init] WARN: skipping invalid address %q: %v", e.Address, err)
			continue
		}
		pk, err := hex.DecodeString(e.PrivateKey)
		if err != nil {
			log.Printf("[init] WARN: skipping address %s, bad private key hex: %v", e.Address, err)
			continue
		}
		keystore[addr] = &types.KeyInfo{
			Type:       types.KTSecp256k1,
			PrivateKey: pk,
		}
		addrs = append(addrs, addr)
	}

	if len(addrs) == 0 {
		log.Fatal("[init] FATAL: no valid keys loaded from keystore")
	}
	log.Printf("[init] loaded %d keys from keystore", len(addrs))
}

func waitForChain() {
	targetHeight := envInt("STRESS_WAIT_HEIGHT", 10)
	node := nodes[nodeKeys[0]]
	log.Printf("[init] waiting for chain height >= %d ...", targetHeight)

	for {
		head, err := node.ChainHead(ctx)
		if err != nil {
			log.Printf("[init] ChainHead error: %v, retrying...", err)
			time.Sleep(2 * time.Second)
			continue
		}
		if int(head.Height()) >= targetHeight {
			log.Printf("[init] chain at height %d, proceeding", head.Height())
			return
		}
		log.Printf("[init] chain at height %d, waiting...", head.Height())
		time.Sleep(2 * time.Second)
	}
}

func initNonces() {
	node := nodes[nodeKeys[0]]
	for _, addr := range addrs {
		n, err := node.MpoolGetNonce(ctx, addr)
		if err != nil {
			log.Printf("[init] WARN: cannot get nonce for %s: %v, starting at 0", addr, err)
			nonces[addr] = 0
			continue
		}
		nonces[addr] = n
	}
	log.Printf("[init] initialized nonces for %d addresses", len(addrs))
}

// ---------------------------------------------------------------------------
// Deck building
// ---------------------------------------------------------------------------

func buildDeck() {
	type weightedAction struct {
		name      string
		envVar    string
		fn        func()
		defWeight int
	}

	actions := []weightedAction{
		{"DoTransferMarket", "STRESS_WEIGHT_TRANSFER", DoTransferMarket, 10},
		{"DoSharedState", "STRESS_WEIGHT_SHARED_STATE", DoSharedState, 1},
		{"DoGasWar", "STRESS_WEIGHT_GAS_WAR", DoGasWar, 2},
		{"DoHeavyCompute", "STRESS_WEIGHT_HEAVY_COMPUTE", DoHeavyCompute, 1},
		{"DoAdversarial", "STRESS_WEIGHT_ADVERSARIAL", DoAdversarial, 2},
		{"DoChainMonitor", "STRESS_WEIGHT_CHAIN_MONITOR", DoChainMonitor, 3},
	}

	deck = nil
	for _, a := range actions {
		w := envInt(a.envVar, a.defWeight)
		if w > 0 {
			log.Printf("[init] action %s: weight=%d", a.name, w)
		}
		for i := 0; i < w; i++ {
			deck = append(deck, namedAction{name: a.name, fn: a.fn})
		}
	}

	if len(deck) == 0 {
		log.Fatal("[init] FATAL: deck is empty — set at least one STRESS_WEIGHT_* > 0")
	}
	log.Printf("[init] deck built with %d entries", len(deck))
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("[engine] stress engine starting")

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	connectNodes()
	loadKeystore()
	waitForChain()
	initNonces()
	buildDeck()

	lifecycle.SetupComplete(map[string]any{
		"nodes":   len(nodes),
		"wallets": len(addrs),
		"deck":    len(deck),
	})

	log.Println("[engine] entering main loop")

	// Track action execution counts for periodic summary
	actionCounts := make(map[string]int)
	iteration := 0

	for {
		idx := rngIntn(len(deck))
		action := deck[idx]

		// Log every action execution
		log.Printf("[engine] running: %s", action.name)
		action.fn()

		actionCounts[action.name]++
		iteration++

		// Periodic summary every 100 iterations
		if iteration%100 == 0 {
			log.Printf("[engine] === iteration %d summary ===", iteration)
			for name, count := range actionCounts {
				log.Printf("[engine]   %s: %d", name, count)
			}
		}
	}
}
