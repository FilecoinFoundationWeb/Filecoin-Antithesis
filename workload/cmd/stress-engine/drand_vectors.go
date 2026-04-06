package main

import (
	"encoding/hex"
	"fmt"
	"log"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/filecoin-project/go-state-types/abi"
)

// ===========================================================================
// DoDrandBeaconAudit — Cross-node drand beacon entry consistency
//
// Picks a random finalized height, collects BeaconEntries from every node's
// block headers at that height, and asserts they are identical. Beacon
// entries are deterministic (drand round → BLS signature) so any mismatch
// in finalized blocks indicates a consensus or drand integration bug.
//
// Covers issue #229 scenario 7 (beacon entry audit) and validates
// lotus#11500 concern 1 (correctness of drand usage across implementations).
// ===========================================================================

func DoDrandBeaconAudit() {
	if len(nodeKeys) < 2 {
		return
	}
	if !allNodesPastEpoch(f3MinEpoch) {
		return
	}

	snap := getFinalizedSnapshots()
	finalizedHeight, _ := snapshotMinHeight(snap)
	if finalizedHeight < finalizedMinHeight {
		return
	}

	// Pick a random finalized height to audit
	checkHeight := abi.ChainEpoch(rngIntn(int(finalizedHeight)) + 1)

	// Collect beacon fingerprints from each node at this height.
	// Fingerprint = "round:hex(data)" for the last beacon entry in the first block.
	type beaconResult struct {
		fingerprint string
		round       uint64
		sigPrefix   string // first 16 hex chars for logging
	}

	results := make(map[string]beaconResult) // nodeName -> result
	var errs int

	for name, s := range snap {
		if s.err != nil {
			errs++
			continue
		}

		ts, err := nodes[name].ChainGetTipSetByHeight(ctx, checkHeight, s.key)
		if err != nil {
			log.Printf("[drand-audit] ChainGetTipSetByHeight(%d) failed on %s: %v", checkHeight, name, err)
			errs++
			continue
		}

		blks := ts.Blocks()
		if len(blks) == 0 {
			continue
		}

		// All blocks in a tipset share the same beacon entries; use the first block.
		be := blks[0].BeaconEntries
		if len(be) == 0 {
			// Null rounds or very early epochs may have no beacon entries
			results[name] = beaconResult{fingerprint: "empty", round: 0, sigPrefix: ""}
			continue
		}

		// Use the last (most recent) beacon entry for this epoch
		latest := be[len(be)-1]
		sig := hex.EncodeToString(latest.Data)
		fp := fmt.Sprintf("%d:%s", latest.Round, sig)

		prefix := sig
		if len(prefix) > 16 {
			prefix = prefix[:16]
		}

		results[name] = beaconResult{
			fingerprint: fp,
			round:       latest.Round,
			sigPrefix:   prefix,
		}
	}

	responded := len(results)
	if responded < 2 {
		return
	}

	// Group by fingerprint
	groups := make(map[string][]string) // fingerprint -> []nodeName
	for name, r := range results {
		groups[r.fingerprint] = append(groups[r.fingerprint], name)
	}

	allMatch := len(groups) == 1

	// Pick a sample for logging
	var sampleRound uint64
	var sampleSig string
	for _, r := range results {
		sampleRound = r.round
		sampleSig = r.sigPrefix
		break
	}

	assert.Always(allMatch, "Drand beacon entries match across all nodes at finalized height", map[string]any{
		"height":          checkHeight,
		"finalized_at":    finalizedHeight,
		"beacon_round":    sampleRound,
		"beacon_sig":      sampleSig,
		"unique_beacons":  len(groups),
		"nodes_checked":   responded,
		"errors":          errs,
	})

	if !allMatch {
		log.Printf("[drand-audit] MISMATCH at height %d: %d unique beacon entries across %d nodes",
			checkHeight, len(groups), responded)
		for fp, names := range groups {
			log.Printf("[drand-audit]   fingerprint=%s nodes=%v", fp[:min(40, len(fp))], names)
		}
	} else {
		debugLog("[drand-audit] height=%d beacon_round=%d sig=%s nodes=%d OK",
			checkHeight, sampleRound, sampleSig, responded)
	}
}
