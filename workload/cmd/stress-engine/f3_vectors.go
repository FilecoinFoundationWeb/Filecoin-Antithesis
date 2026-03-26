package main

import (
	"bytes"
	"log"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-f3/certs"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

// ===========================================================================
// DoF3ECConsistency — F3 Certificate vs EC Canonical Chain Consistency
// ===========================================================================
//
// This vector demonstrates the core value of F3 over EC: even under adversarial
// conditions (the adversary node holds ~16.7% power and broadcasts invalid
// blocks), F3-certified tipsets must always match what EC considers canonical.
//
// Assertions:
//   - always: F3 certified epoch ≤ EC head height (no future certification)
//   - always: F3-certified tipset key matches EC canonical tipset at that epoch
//   - always: all nodes agree on the same latest F3 certificate
//   - sometimes: F3 has issued at least one certificate (liveness)
//
// The safety assertions confirm Byzantine block spam cannot cause F3 to certify
// a fork that diverges from EC's canonical chain.
func DoF3ECConsistency() {
	type certResult struct {
		node      string
		instance  uint64
		certKey   []byte
		certEpoch abi.ChainEpoch
	}

	var results []certResult
	for _, name := range nodeKeys {
		if nodeType(name) != "lotus" {
			continue
		}
		cert, err := nodes[name].F3GetLatestCertificate(ctx)
		if err != nil {
			debugLog("[f3-ec] F3GetLatestCertificate failed on %s: %v", name, err)
			continue
		}
		if cert == nil {
			continue
		}
		if cert.ECChain == nil || len(cert.ECChain.TipSets) == 0 {
			continue
		}
		head := cert.ECChain.Head()
		results = append(results, certResult{
			node:      name,
			instance:  cert.GPBFTInstance,
			certKey:   head.Key,
			certEpoch: abi.ChainEpoch(head.Epoch),
		})
	}

	// Liveness: F3 must eventually issue at least one certificate.
	assert.Sometimes(len(results) > 0, "F3 has issued at least one finality certificate", map[string]any{
		"nodes_checked": len(nodeKeys),
	})

	if len(results) == 0 {
		return
	}

	// Cross-node cert agreement: all nodes must agree on the same instance and key.
	first := results[0]
	for _, r := range results[1:] {
		instanceMatch := r.instance == first.instance
		keyMatch := bytes.Equal(r.certKey, first.certKey)
		assert.Always(instanceMatch && keyMatch, "All nodes agree on the latest F3 certificate", map[string]any{
			"node_a":        first.node,
			"node_b":        r.node,
			"instance_a":    first.instance,
			"instance_b":    r.instance,
			"key_a":         first.certKey,
			"key_b":         r.certKey,
			"instance_match": instanceMatch,
			"key_match":     keyMatch,
		})
	}

	// Use the primary lotus node for EC chain lookups.
	var primaryNode api.FullNode
	for _, name := range nodeKeys {
		if nodeType(name) == "lotus" {
			primaryNode = nodes[name]
			break
		}
	}
	if primaryNode == nil {
		return
	}

	ecHead, err := primaryNode.ChainHead(ctx)
	if err != nil {
		log.Printf("[f3-ec] ChainHead failed: %v", err)
		return
	}

	// Safety: F3 cannot certify a tipset at an epoch beyond the current EC head.
	assert.Always(first.certEpoch <= ecHead.Height(), "F3 certified epoch must not exceed EC head height", map[string]any{
		"cert_epoch": first.certEpoch,
		"ec_height":  ecHead.Height(),
		"instance":   first.instance,
	})

	// Safety: the tipset F3 certified must be the same tipset EC considers
	// canonical at that epoch. A Byzantine actor cannot get F3 to certify a fork.
	ecTs, err := primaryNode.ChainGetTipSetByHeight(ctx, first.certEpoch, ecHead.Key())
	if err != nil {
		log.Printf("[f3-ec] ChainGetTipSetByHeight(%d) failed: %v", first.certEpoch, err)
		return
	}

	f3Tsk, err := types.TipSetKeyFromBytes(first.certKey)
	if err != nil {
		log.Printf("[f3-ec] TipSetKeyFromBytes failed: %v", err)
		return
	}

	keysMatch := ecTs.Key() == f3Tsk
	assert.Always(keysMatch, "F3-certified tipset matches EC canonical chain at that epoch", map[string]any{
		"cert_epoch": first.certEpoch,
		"instance":   first.instance,
		"f3_tsk":    f3Tsk.String(),
		"ec_tsk":    ecTs.Key().String(),
	})

	if keysMatch {
		debugLog("[f3-ec] instance=%d epoch=%d: F3 cert consistent with EC canonical chain", first.instance, first.certEpoch)
	}
}

// getLatestCert is a thin helper for callers that only need the cert from one node.
func getLatestCert(node api.FullNode) *certs.FinalityCertificate {
	cert, err := node.F3GetLatestCertificate(ctx)
	if err != nil {
		debugLog("[f3-ec] F3GetLatestCertificate: %v", err)
		return nil
	}
	return cert
}
