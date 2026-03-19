package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-state-types/abi"
)

// ===========================================================================
// Vector: DoAdversaryNodeAttack
//
// Exercises the lotus-adversary0 node's built-in attack API (AdversaryEnable,
// AdversaryDisable, AdversaryList) and verifies that honest nodes maintain
// consensus despite invalid block spam.
//
// The adversary node has a custom RPC API that must be enabled via the
// LOTUS_ADVERSARY_ENABLEADVERSARYAPI=true environment variable. Attacks
// include invalid-block-spammer which publishes blocks with valid CBOR
// but invalid crypto proofs.
//
// Sub-vectors (randomly selected per invocation):
//   0. Toggle: enable, verify active, sleep, disable — API smoke test
//   1. Spam consensus: enable spammer, wait, compare state roots on honest nodes
//   2. Isolation: enable spammer, partition adversary, reconnect, verify convergence
//   3. Variable intensity: enable spammer with random blocksPerEpoch, verify consensus
// ===========================================================================

const (
	adversaryNodeName = "lotus-adversary0"
	adversaryRPCURL   = "http://lotus-adversary0:1234/rpc/v0"
	adversaryJWTPath  = "/root/devgen/lotus-adversary0/lotus-adversary0-jwt"

	adversaryAttackName = "invalid-block-spammer"
)

var (
	adversaryAttackActive bool
	adversaryMu           sync.Mutex
)

// ---------------------------------------------------------------------------
// Raw JSON-RPC helper (adversary methods aren't in api.FullNode interface)
// ---------------------------------------------------------------------------

type jsonRPCRequest struct {
	Jsonrpc string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

type jsonRPCResponse struct {
	Jsonrpc string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *jsonRPCError   `json:"error"`
	ID      int             `json:"id"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// adversaryRPC makes a raw JSON-RPC call to the adversary node.
func adversaryRPC(method string, params []interface{}) (json.RawMessage, error) {
	token, err := os.ReadFile(adversaryJWTPath)
	if err != nil {
		return nil, fmt.Errorf("read JWT: %w", err)
	}
	jwt := strings.TrimSpace(string(token))

	req := jsonRPCRequest{
		Jsonrpc: "2.0",
		Method:  "Filecoin." + method,
		Params:  params,
		ID:      1,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", adversaryRPCURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if jwt != "" {
		httpReq.Header.Set("Authorization", "Bearer "+jwt)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(respBody))
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// ---------------------------------------------------------------------------
// Convenience wrappers
// ---------------------------------------------------------------------------

func adversaryEnable(attack string, params map[string]interface{}) error {
	args := []interface{}{attack}
	if params != nil {
		args = append(args, params)
	}
	_, err := adversaryRPC("AdversaryEnable", args)
	return err
}

func adversaryDisable(attack string) error {
	_, err := adversaryRPC("AdversaryDisable", []interface{}{attack})
	return err
}

func adversaryList() ([]string, error) {
	result, err := adversaryRPC("AdversaryList", nil)
	if err != nil {
		return nil, err
	}
	var attacks []string
	if err := json.Unmarshal(result, &attacks); err != nil {
		return nil, fmt.Errorf("unmarshal AdversaryList: %w", err)
	}
	return attacks, nil
}

// ---------------------------------------------------------------------------
// Honest node filtering
// ---------------------------------------------------------------------------

// honestNodeKeys returns nodeKeys excluding the adversary node.
// The adversary's state may intentionally diverge, so it must be excluded
// from consensus checks.
func honestNodeKeys() []string {
	honest := make([]string, 0, len(nodeKeys))
	for _, k := range nodeKeys {
		if k != adversaryNodeName {
			honest = append(honest, k)
		}
	}
	return honest
}

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

func DoAdversaryNodeAttack() {
	// Verify the adversary node is in our node pool
	if _, ok := nodes[adversaryNodeName]; !ok {
		debugLog("[adversary] node %s not in pool, skipping", adversaryNodeName)
		return
	}

	// Check the adversary API is reachable before proceeding
	_, err := adversaryList()
	if err != nil {
		log.Printf("[adversary] API not reachable (is LOTUS_ADVERSARY_ENABLEADVERSARYAPI=true?): %v", err)
		return
	}

	subAction := rngIntn(4)
	subNames := []string{"toggle", "spam-consensus", "isolation", "variable-intensity"}
	log.Printf("[adversary] sub-action: %s", subNames[subAction])

	switch subAction {
	case 0:
		doAdversaryToggleAttack()
	case 1:
		doAdversarySpamConsensus()
	case 2:
		doAdversaryIsolation()
	case 3:
		doAdversaryVariableIntensity()
	}
}

// ---------------------------------------------------------------------------
// Sub-vector 0: Toggle attack (API smoke test)
// ---------------------------------------------------------------------------

func doAdversaryToggleAttack() {
	adversaryMu.Lock()
	if adversaryAttackActive {
		adversaryMu.Unlock()
		log.Printf("[adversary-toggle] attack already active, skipping")
		return
	}
	adversaryAttackActive = true
	adversaryMu.Unlock()

	defer func() {
		adversaryMu.Lock()
		adversaryAttackActive = false
		adversaryMu.Unlock()
	}()

	// Enable the invalid block spammer
	err := adversaryEnable(adversaryAttackName, nil)
	enableOK := err == nil

	assert.Sometimes(enableOK, "Adversary attack enable succeeded", map[string]any{
		"attack": adversaryAttackName,
		"error":  errStr(err),
	})

	if !enableOK {
		log.Printf("[adversary-toggle] enable failed: %v", err)
		return
	}
	log.Printf("[adversary-toggle] enabled %s", adversaryAttackName)

	// Verify it shows up in the active list
	active, err := adversaryList()
	if err == nil {
		found := false
		for _, a := range active {
			if a == adversaryAttackName {
				found = true
				break
			}
		}
		assert.Sometimes(found, "Adversary attack appears in active list after enable", map[string]any{
			"attack":      adversaryAttackName,
			"active_list": active,
		})
		log.Printf("[adversary-toggle] active attacks: %v", active)
	}

	// Let it run briefly: 2-7 seconds
	sleepDur := time.Duration(rngIntn(6)+2) * time.Second
	time.Sleep(sleepDur)

	// Disable
	err = adversaryDisable(adversaryAttackName)
	if err != nil {
		log.Printf("[adversary-toggle] disable failed: %v", err)
	} else {
		log.Printf("[adversary-toggle] disabled %s after %s", adversaryAttackName, sleepDur)
	}
}

// ---------------------------------------------------------------------------
// Sub-vector 1: Spam consensus (verify honest nodes agree despite spam)
// ---------------------------------------------------------------------------

func doAdversarySpamConsensus() {
	adversaryMu.Lock()
	if adversaryAttackActive {
		adversaryMu.Unlock()
		log.Printf("[adversary-spam] attack already active, skipping")
		return
	}
	adversaryAttackActive = true
	adversaryMu.Unlock()

	defer func() {
		adversaryMu.Lock()
		adversaryAttackActive = false
		adversaryMu.Unlock()
	}()

	// Random blocks per epoch: 1-5
	bpe := rngIntn(5) + 1
	params := map[string]interface{}{"blocksPerEpoch": bpe}

	err := adversaryEnable(adversaryAttackName, params)
	if err != nil {
		log.Printf("[adversary-spam] enable failed: %v", err)
		return
	}
	log.Printf("[adversary-spam] enabled %s (blocksPerEpoch=%d)", adversaryAttackName, bpe)

	// Let the spammer run for 30 seconds
	time.Sleep(30 * time.Second)

	// Disable before checking
	if err := adversaryDisable(adversaryAttackName); err != nil {
		log.Printf("[adversary-spam] disable failed: %v", err)
	}

	// Compare finalized state roots across honest nodes
	honest := honestNodeKeys()
	if len(honest) < 2 {
		log.Printf("[adversary-spam] need >=2 honest nodes for consensus check, have %d", len(honest))
		return
	}

	stateRoots := make(map[string][]string)
	for _, name := range honest {
		finTs, err := nodes[name].ChainGetFinalizedTipSet(ctx)
		if err != nil {
			log.Printf("[adversary-spam] ChainGetFinalizedTipSet failed for %s: %v", name, err)
			return
		}
		root := finTs.Blocks()[0].ParentStateRoot.String()
		stateRoots[root] = append(stateRoots[root], name)
	}

	statesMatch := len(stateRoots) == 1

	assert.Always(statesMatch, "Honest nodes maintain consensus despite invalid block spam", map[string]any{
		"attack":        adversaryAttackName,
		"blocksPerEpoch": bpe,
		"unique_states": len(stateRoots),
		"state_roots":   stateRoots,
		"honest_nodes":  honest,
	})

	if statesMatch {
		log.Printf("[adversary-spam] OK: honest nodes agree on finalized state (bpe=%d)", bpe)
	} else {
		log.Printf("[adversary-spam] DIVERGENCE: honest nodes disagree: %v", stateRoots)
	}
}

// ---------------------------------------------------------------------------
// Sub-vector 2: Isolation (partition adversary while attacking)
// ---------------------------------------------------------------------------

func doAdversaryIsolation() {
	adversaryMu.Lock()
	if adversaryAttackActive {
		adversaryMu.Unlock()
		log.Printf("[adversary-isolate] attack already active, skipping")
		return
	}
	adversaryAttackActive = true
	adversaryMu.Unlock()

	defer func() {
		adversaryMu.Lock()
		adversaryAttackActive = false
		adversaryMu.Unlock()
	}()

	adversary := nodes[adversaryNodeName]

	// Enable the spammer
	err := adversaryEnable(adversaryAttackName, nil)
	if err != nil {
		log.Printf("[adversary-isolate] enable failed: %v", err)
		return
	}
	log.Printf("[adversary-isolate] enabled %s", adversaryAttackName)

	// Partition: disconnect adversary from all peers
	peers, err := adversary.NetPeers(ctx)
	if err != nil {
		log.Printf("[adversary-isolate] NetPeers failed: %v", err)
		adversaryDisable(adversaryAttackName)
		return
	}

	disconnected := 0
	for _, p := range peers {
		if err := adversary.NetDisconnect(ctx, p.ID); err == nil {
			disconnected++
		}
	}
	log.Printf("[adversary-isolate] partitioned adversary (disconnected %d/%d peers)", disconnected, len(peers))

	// Wait 3 epochs on honest chain
	honest := honestNodeKeys()
	if len(honest) > 0 {
		waitForEpochsOnOther(adversaryNodeName, 3)
	} else {
		time.Sleep(18 * time.Second) // fallback ~3 epochs at 6s/epoch
	}

	// Reconnect adversary
	knownPeers := collectNodeAddrInfos(adversaryNodeName)
	reconnected := 0
	for _, p := range knownPeers {
		if err := adversary.NetConnect(ctx, p); err == nil {
			reconnected++
		}
	}
	log.Printf("[adversary-isolate] reconnected adversary (%d/%d peers)", reconnected, len(knownPeers))

	// Disable the attack
	if err := adversaryDisable(adversaryAttackName); err != nil {
		log.Printf("[adversary-isolate] disable failed: %v", err)
	}

	// Wait for convergence among honest nodes
	log.Printf("[adversary-isolate] waiting for honest node convergence...")
	converged := waitForConvergence(adversaryNodeName)

	assert.Sometimes(converged, "Honest nodes converge after adversary isolation and reconnect", map[string]any{
		"attack":       adversaryAttackName,
		"disconnected": disconnected,
		"reconnected":  reconnected,
	})

	if converged {
		log.Printf("[adversary-isolate] OK: honest nodes converged after adversary isolation")
	} else {
		log.Printf("[adversary-isolate] WARN: convergence not achieved after adversary isolation")
	}
}

// ---------------------------------------------------------------------------
// Sub-vector 3: Variable intensity (scale blocksPerEpoch)
// ---------------------------------------------------------------------------

func doAdversaryVariableIntensity() {
	adversaryMu.Lock()
	if adversaryAttackActive {
		adversaryMu.Unlock()
		log.Printf("[adversary-intensity] attack already active, skipping")
		return
	}
	adversaryAttackActive = true
	adversaryMu.Unlock()

	defer func() {
		adversaryMu.Lock()
		adversaryAttackActive = false
		adversaryMu.Unlock()
	}()

	intensities := []int{1, 5, 20, 50}
	bpe := rngChoice(intensities)

	// Scale duration: higher intensity = shorter run to limit damage
	// 1 bpe → 30s, 5 bpe → 20s, 20 bpe → 15s, 50 bpe → 10s
	var duration time.Duration
	switch {
	case bpe <= 1:
		duration = 30 * time.Second
	case bpe <= 5:
		duration = 20 * time.Second
	case bpe <= 20:
		duration = 15 * time.Second
	default:
		duration = 10 * time.Second
	}

	params := map[string]interface{}{"blocksPerEpoch": bpe}
	err := adversaryEnable(adversaryAttackName, params)
	if err != nil {
		log.Printf("[adversary-intensity] enable failed: %v", err)
		return
	}
	log.Printf("[adversary-intensity] enabled %s (bpe=%d, duration=%s)", adversaryAttackName, bpe, duration)

	time.Sleep(duration)

	if err := adversaryDisable(adversaryAttackName); err != nil {
		log.Printf("[adversary-intensity] disable failed: %v", err)
	}

	// Verify honest consensus
	honest := honestNodeKeys()
	if len(honest) < 2 {
		return
	}

	// Brief pause for finalization to catch up
	time.Sleep(5 * time.Second)

	stateRoots := make(map[string][]string)
	var checkHeight abi.ChainEpoch
	for _, name := range honest {
		finTs, err := nodes[name].ChainGetFinalizedTipSet(ctx)
		if err != nil {
			log.Printf("[adversary-intensity] ChainGetFinalizedTipSet failed for %s: %v", name, err)
			return
		}
		if checkHeight == 0 {
			checkHeight = finTs.Height()
		}
		// Use the finalized tipset's parent state root at each node's finalized height
		root := finTs.Blocks()[0].ParentStateRoot.String()
		stateRoots[root] = append(stateRoots[root], name)
	}

	statesMatch := len(stateRoots) == 1

	assert.Always(statesMatch, "Honest nodes maintain consensus despite variable-intensity spam", map[string]any{
		"attack":         adversaryAttackName,
		"blocksPerEpoch": bpe,
		"duration":       duration.String(),
		"unique_states":  len(stateRoots),
		"state_roots":    stateRoots,
		"honest_nodes":   honest,
	})

	if statesMatch {
		log.Printf("[adversary-intensity] OK: honest consensus maintained (bpe=%d, duration=%s)", bpe, duration)
	} else {
		log.Printf("[adversary-intensity] DIVERGENCE at bpe=%d: %v", bpe, stateRoots)
	}
}
