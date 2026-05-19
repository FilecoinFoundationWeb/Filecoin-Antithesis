package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"workload/internal/chain"
	"workload/internal/foc"

	"github.com/antithesishq/antithesis-sdk-go/random"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	_ "github.com/filecoin-project/lotus/lib/sigs/secp"
	"github.com/ipfs/go-cid"
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
	addrs    []address.Address // deck wallets (background operations)
	atkAddrs []address.Address // attack-reserved wallets (nsplit only)

	// Per-address monotonic nonce counter
	nonces map[address.Address]uint64

	// Weighted action deck with names for logging
	deck []namedAction

	// Deployed contract registry (protected by contractsMu)
	deployedContracts []deployedContract
	contractsMu       sync.Mutex

	// Contract bytecodes (loaded from embedded hex in contracts.go)
	contractBytecodes map[string][]byte
	contractTypes     []string // keys of contractBytecodes for random selection

	// Pending deploy CIDs for deferred verification
	pendingDeploys []pendingDeploy
	pendingMu      sync.Mutex

	// FOC config — nil when the FOC compose profile is not active
	focCfg *foc.Config
)

type deployedContract struct {
	addr     address.Address
	ctype    string // "recursive", "selfdestruct", "simplecoin", etc.
	deployer address.Address
	deployKI *types.KeyInfo
}

type pendingDeploy struct {
	msgCid   cid.Cid
	ctype    string
	deployer address.Address
	deployKI *types.KeyInfo
	epoch    abi.ChainEpoch
}

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

// pickAttackWallet returns a wallet from the attack-reserved pool.
// These wallets are never used by deck vectors, so their nonces remain
// stable on isolated nodes during network partitions.
func pickAttackWallet() (address.Address, *types.KeyInfo) {
	addr := rngChoice(atkAddrs)
	return addr, keystore[addr]
}

// ---------------------------------------------------------------------------
// Initialization
// ---------------------------------------------------------------------------

func connectNodes() {
	cfg := chain.NodeConfig{
		Names:      strings.Split(envOrDefault("STRESS_NODES", "lotus0"), ","),
		Port:       envOrDefault("STRESS_RPC_PORT", "1234"),
		ForestPort: envOrDefault("STRESS_FOREST_RPC_PORT", "3456"),
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

	// Reserve last 10 wallets (or 10% if fewer) for attack injection.
	// These are never used by deck vectors, so their nonces stay stable
	// across network partitions — critical for full-isolation nsplit tests.
	atkCount := 10
	if atkCount > len(addrs)/5 {
		atkCount = len(addrs) / 5
	}
	if atkCount < 2 {
		atkCount = 2
	}
	if atkCount > len(addrs) {
		atkCount = len(addrs) / 2
	}
	splitIdx := len(addrs) - atkCount
	atkAddrs = addrs[splitIdx:]
	addrs = addrs[:splitIdx]
	log.Printf("[init] loaded %d deck keys + %d attack-reserved keys from keystore", len(addrs), len(atkAddrs))
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
	// Don't use append(addrs, atkAddrs...) — they share a backing array.
	allAddrs := make([]address.Address, 0, len(addrs)+len(atkAddrs))
	allAddrs = append(allAddrs, addrs...)
	allAddrs = append(allAddrs, atkAddrs...)
	for _, addr := range allAddrs {
		n, err := node.MpoolGetNonce(ctx, addr)
		if err != nil {
			log.Printf("[init] WARN: cannot get nonce for %s: %v, starting at 0", addr, err)
			nonces[addr] = 0
			continue
		}
		nonces[addr] = n
	}
	log.Printf("[init] initialized nonces for %d addresses (%d deck + %d attack)",
		len(allAddrs), len(addrs), len(atkAddrs))
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

	// Consensus / health-check vectors — always active in both profiles
	consensus := []weightedAction{
		{"DoTipsetConsensus", "STRESS_WEIGHT_TIPSET_CONSENSUS", DoTipsetConsensus, 3},
		{"DoHeightProgression", "STRESS_WEIGHT_HEIGHT_PROGRESSION", DoHeightProgression, 2},
		{"DoPeerCount", "STRESS_WEIGHT_PEER_COUNT", DoPeerCount, 2},
		{"DoHeadComparison", "STRESS_WEIGHT_HEAD_COMPARISON", DoHeadComparison, 3},
		{"DoStateRootComparison", "STRESS_WEIGHT_STATE_ROOT", DoStateRootComparison, 4},
		{"DoStateAudit", "STRESS_WEIGHT_STATE_AUDIT", DoStateAudit, 5},
		{"DoF3FinalityMonitor", "STRESS_WEIGHT_F3_MONITOR", DoF3FinalityMonitor, 2},
		{"DoF3FinalityAgreement", "STRESS_WEIGHT_F3_AGREEMENT", DoF3FinalityAgreement, 3},
		{"DoDrandBeaconAudit", "STRESS_WEIGHT_DRAND_BEACON_AUDIT", DoDrandBeaconAudit, 3},
		// Quiet recovery: pauses all faults, checks self-healing (off by default, enable via QUIET_RECOVERY_ENABLED=1)
		{"DoQuietRecovery", "STRESS_WEIGHT_QUIET_RECOVERY", DoQuietRecovery, 0},
	}

	// Network upgrade suite — single entry, runs all sub-vectors per invocation.
	upgrade := []weightedAction{
		{"DoUpgradeSuite", "STRESS_WEIGHT_UPGRADE_SUITE", DoUpgradeSuite, 0},
	}

	// Non-FOC stress vectors — skipped when FOC profile is active.
	// All weights are overridable via STRESS_WEIGHT_* env vars from the profile .env.
	stress := []weightedAction{
		// Power table manipulation
		{"DoPowerAwareSlash", "STRESS_WEIGHT_POWER_SLASH", DoPowerAwareSlash, 0},
		// Background chain activity
		{"DoTransferMarket", "STRESS_WEIGHT_TRANSFER", DoTransferMarket, 2},
		{"DoGasWar", "STRESS_WEIGHT_GAS_WAR", DoGasWar, 1},
		{"DoNonceRace", "STRESS_WEIGHT_NONCE_RACE", doNonceRace, 1},
		{"DoHeavyCompute", "STRESS_WEIGHT_HEAVY_COMPUTE", DoHeavyCompute, 1},
		// Cross-node consistency
		{"DoReceiptAudit", "STRESS_WEIGHT_RECEIPT_AUDIT", DoReceiptAudit, 2},
		// EVM contract stress
		{"DoDeployContracts", "STRESS_WEIGHT_DEPLOY", DoDeployContracts, 1},
		{"DoContractCall", "STRESS_WEIGHT_CONTRACT_CALL", DoContractCall, 1},
		{"DoSelfDestructCycle", "STRESS_WEIGHT_SELFDESTRUCT", DoSelfDestructCycle, 0},
		{"DoConflictingContractCalls", "STRESS_WEIGHT_CONTRACT_RACE", DoConflictingContractCalls, 1},
		{"DoMaxBlockGas", "STRESS_WEIGHT_MAX_BLOCK_GAS", DoMaxBlockGas, 0},
		{"DoLogBlaster", "STRESS_WEIGHT_LOG_BLASTER", DoLogBlaster, 0},
		{"DoMemoryBomb", "STRESS_WEIGHT_MEMORY_BOMB", DoMemoryBomb, 0},
		{"DoStorageSpam", "STRESS_WEIGHT_STORAGE_SPAM", DoStorageSpam, 0},
		// Mempool safety
		{"DoDoubleSpend", "STRESS_WEIGHT_DOUBLE_SPEND", doDoubleSpend, 1},
		{"DoInvalidSignature", "STRESS_WEIGHT_INVALID_SIG", doInvalidSignature, 1},
		// Cross-node divergence
		{"DoMessageOrderingAttack", "STRESS_WEIGHT_MSG_ORDERING", DoMessageOrderingAttack, 1},
		{"DoNonceBombard", "STRESS_WEIGHT_NONCE_BOMBARD", DoNonceBombard, 0},
		{"DoGasExhaustionEdge", "STRESS_WEIGHT_GAS_EXHAUST", DoGasExhaustionEdge, 0},
		// State tree stress
		{"DoActorMigrationStress", "STRESS_WEIGHT_ACTOR_MIGRATION", DoActorMigrationStress, 1},
		{"DoActorLifecycleStress", "STRESS_WEIGHT_ACTOR_LIFECYCLE", DoActorLifecycleStress, 1},
		// Cross-implementation (Lotus ↔ Forest)
		{"DoCrossImplStateCompute", "STRESS_WEIGHT_CROSS_IMPL_COMPUTE", DoCrossImplStateCompute, 2},
		{"DoDeepActorStateComparison", "STRESS_WEIGHT_DEEP_ACTOR_STATE", DoDeepActorStateComparison, 1},
		{"DoCrossImplEthCall", "STRESS_WEIGHT_CROSS_IMPL_ETH_CALL", DoCrossImplEthCall, 1},
		// FIP-specific: post-activation behavior probes
		{"DoFIP0115BaseFeeResponse", "STRESS_WEIGHT_FIP0115", DoFIP0115BaseFeeResponse, 0},
		// Reorg chaos (guarded by partitionActive to avoid stomping n-split)
		{"DoReorgChaos", "STRESS_WEIGHT_REORG", DoReorgChaos, 0},
	}

	// Build actions list: consensus always, upgrade always, stress only when FOC is not active
	actions := append([]weightedAction{}, consensus...)
	actions = append(actions, upgrade...)
	if focCfg == nil {
		actions = append(actions, stress...)
	} else {
		log.Println("[init] FOC active — skipping non-FOC stress vectors (covered by filecoin run)")
	}

	// FOC lifecycle vectors — only when FOC profile is active
	if focCfg != nil {
		actions = append(actions,
			// Sequential lifecycle state machine (drives setup to completion)
			weightedAction{"DoFOCLifecycle", "STRESS_WEIGHT_FOC_LIFECYCLE", DoFOCLifecycle, 3},
			// Steady-state vectors (only fire once lifecycle reaches Ready)
			weightedAction{"DoFOCUploadPiece", "STRESS_WEIGHT_FOC_UPLOAD", DoFOCUploadPiece, 2},
			weightedAction{"DoFOCAddPieces", "STRESS_WEIGHT_FOC_ADD_PIECES", DoFOCAddPieces, 1},
			weightedAction{"DoFOCMonitorProofSet", "STRESS_WEIGHT_FOC_MONITOR", DoFOCMonitorProofSet, 3},
			weightedAction{"DoFOCRetrieveAndVerify", "STRESS_WEIGHT_FOC_RETRIEVE", DoFOCRetrieveAndVerify, 1},
			weightedAction{"DoFOCTransfer", "STRESS_WEIGHT_FOC_TRANSFER", DoFOCTransfer, 1},
			weightedAction{"DoFOCSettle", "STRESS_WEIGHT_FOC_SETTLE", DoFOCSettle, 1},
			weightedAction{"DoFOCWithdraw", "STRESS_WEIGHT_FOC_WITHDRAW", DoFOCWithdraw, 1},
			// Shallow reorg chaos — exercises Curio's chain-tracking under reorgs
			weightedAction{"DoReorgChaos", "STRESS_WEIGHT_REORG", DoReorgChaos, 1},
			// Destructive — weight 0 by default (opt-in)
			weightedAction{"DoFOCDeletePiece", "STRESS_WEIGHT_FOC_DELETE_PIECE", DoFOCDeletePiece, 0},
			weightedAction{"DoFOCDeleteDataSet", "STRESS_WEIGHT_FOC_DELETE_DS", DoFOCDeleteDataSet, 0},
		)
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
	initContractBytecodes()
	focCfg = foc.ParseEnvironment()
	buildDeck()

	// Background goroutines — run independently of the deck
	startForkMonitor() // observes forks during partitions
	if focCfg == nil {
		startConsensusTestLifecycle() // structured EC/F3 integration test cycles (skip in FOC — disrupts Curio)
	} else {
		log.Println("[init] FOC active — skipping consensus test lifecycle (n-split partitions)")
	}

	log.Println("[engine] entering main loop")

	// Track action execution counts for periodic summary
	actionCounts := make(map[string]int)
	iteration := 0

	for {
		idx := rngIntn(len(deck))
		action := deck[idx]

		debugLog("[engine] running: %s", action.name)
		action.fn()

		actionCounts[action.name]++
		iteration++

		// Periodic summary every 500 iterations
		if iteration%500 == 0 {
			log.Printf("[engine] === iteration %d summary ===", iteration)
			for name, count := range actionCounts {
				log.Printf("[engine]   %s: %d", name, count)
			}
			if focCfg != nil {
				logFOCProgress()
			}
		}
	}
}
