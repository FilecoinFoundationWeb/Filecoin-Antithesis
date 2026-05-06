package main

import (
	"sync"

	"github.com/antithesishq/antithesis-sdk-go/assert"
)

// ===========================================================================
// DoF3CertMonotonic — Per-Node F3 Certstore Monotonicity
//
// Targets: after F3 quorum loss (e.g. partition isolates
// a majority of participants), the surviving node's certstore can regress:
// F3GetLatestCertificate returns an instance and finalized height much
// lower than what the same node returned moments earlier. Both must be
// strictly non-decreasing per node, even across F3 restarts.
//
// Existing DoF3FinalityAgreement only checks cross-node agreement at a
// single instance; it does not catch a node that goes BACKWARD on its
// own. This vector keeps a per-node history and asserts monotonicity.
//
// We do NOT skip during partitionActive: the regression we want to catch
// is precisely the "partition + heal → certstore reset" sequence. Each
// observation must be >= the prior observation FOR THE SAME NODE,
// regardless of intervening partitions.
// ===========================================================================

type f3CertObs struct {
	instance       uint64
	finalizedEpoch int64
}

var (
	f3CertHistory   = make(map[string]f3CertObs) // nodeName -> last observation
	f3CertHistoryMu sync.Mutex
)

func DoF3CertMonotonic() {
	if !allNodesPastEpoch(f3MinEpoch) {
		return
	}

	for _, name := range nodeKeys {
		// F3GetLatestCertificate is a Lotus-only API in this harness;
		// the Forest gateway exposes it but the per-impl signature
		// can drift. Restrict to lotus to avoid false positives from
		// shape mismatches.
		if nodeType(name) != "lotus" {
			continue
		}

		cert, err := nodes[name].F3GetLatestCertificate(ctx)
		if err != nil || cert == nil {
			debugLog("[f3-mono] %s: F3GetLatestCertificate err=%v cert=%v", name, err, cert)
			continue
		}
		if cert.ECChain == nil || cert.ECChain.IsZero() {
			continue
		}

		head := cert.ECChain.Head()
		if head == nil {
			continue
		}
		obs := f3CertObs{
			instance:       cert.GPBFTInstance,
			finalizedEpoch: head.Epoch,
		}

		f3CertHistoryMu.Lock()
		prev, hadPrev := f3CertHistory[name]
		// Keep only the highest observation we've ever seen per node;
		// updating to a lower value would defeat the monotonicity check.
		if !hadPrev || obs.instance > prev.instance ||
			(obs.instance == prev.instance && obs.finalizedEpoch > prev.finalizedEpoch) {
			f3CertHistory[name] = obs
		}
		f3CertHistoryMu.Unlock()

		if !hadPrev {
			continue
		}

		instanceMonotonic := obs.instance >= prev.instance
		// Within a single instance the finalized epoch may legitimately
		// not change. We only flag a strict regression: instance back
		// AND/OR finalized epoch back.
		epochMonotonic := obs.finalizedEpoch >= prev.finalizedEpoch
		// If the instance moved forward, the finalized epoch is allowed
		// to be equal or higher; never lower.
		mono := instanceMonotonic && epochMonotonic

		details := map[string]any{
			"node":                  name,
			"prev_instance":         prev.instance,
			"prev_finalized_epoch":  prev.finalizedEpoch,
			"obs_instance":          obs.instance,
			"obs_finalized_epoch":   obs.finalizedEpoch,
			"instance_monotonic":    instanceMonotonic,
			"epoch_monotonic":       epochMonotonic,
			"partition_active":      partitionActive.Load(),
		}

		assert.Always(mono,
			"F3 latest certificate is monotonic per node (instance and finalized epoch never regress)",
			details)
	}
}
