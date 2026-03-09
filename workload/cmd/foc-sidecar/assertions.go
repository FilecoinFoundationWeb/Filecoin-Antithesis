package main

import (
	"context"
	"fmt"
	"log"
	"math/big"

	"workload/internal/foc"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/lotus/api"
)

// checkRailToDataset verifies that for every tracked dataset, the on-chain
// railToDataSet(pdpRailId) returns the correct dataSetId.
func checkRailToDataset(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	if cfg.FWSSViewAddr == nil {
		return
	}

	datasets := state.GetDatasets()
	if len(datasets) == 0 {
		return
	}

	for _, ds := range datasets {
		calldata := foc.BuildCalldata(foc.SigRailToDataSet, foc.EncodeUint256(ds.PDPRailID))
		result, err := foc.EthCallUint256(ctx, node, cfg.FWSSViewAddr, calldata)
		if err != nil {
			log.Printf("[rail-to-dataset] railToDataSet(%d) call failed: %v", ds.PDPRailID, err)
			continue
		}

		expected := bigIntFromUint64(ds.DataSetID)
		consistent := result.Cmp(expected) == 0

		assert.Always(consistent, "Rail-to-dataset reverse mapping is consistent", map[string]any{
			"pdpRailId":       ds.PDPRailID,
			"expectedDataSet": ds.DataSetID,
			"actualDataSet":   result.Uint64(),
		})

		if !consistent {
			log.Printf("[rail-to-dataset] VIOLATION: railToDataSet(%d) returned %s, expected %d",
				ds.PDPRailID, result, ds.DataSetID)
		}
	}
}

// checkFilecoinPaySolvency verifies that the FilecoinPay contract holds at
// least as much USDFC as the sum of all tracked accounts' funds + lockup.
func checkFilecoinPaySolvency(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	if cfg.USDFCAddr == nil || cfg.FilPayAddr == nil {
		return
	}

	payers := state.GetTrackedPayers()
	if len(payers) == 0 {
		return
	}

	balCalldata := foc.BuildCalldata(foc.SigBalanceOf, foc.EncodeAddress(cfg.FilPayAddr))
	filPayBalance, err := foc.EthCallUint256(ctx, node, cfg.USDFCAddr, balCalldata)
	if err != nil {
		log.Printf("[solvency] balanceOf(FilecoinPay) failed: %v", err)
		return
	}

	totalOwed := new(big.Int)
	for _, payer := range payers {
		funds := foc.ReadAccountFunds(ctx, node, cfg.FilPayAddr, cfg.USDFCAddr, payer)
		lockup := foc.ReadAccountLockup(ctx, node, cfg.FilPayAddr, cfg.USDFCAddr, payer)
		totalOwed.Add(totalOwed, funds)
		totalOwed.Add(totalOwed, lockup)
	}

	solvent := filPayBalance.Cmp(totalOwed) >= 0

	assert.Always(solvent, "FilecoinPay holds sufficient USDFC (solvency)", map[string]any{
		"filPayBalance": filPayBalance.String(),
		"totalOwed":     totalOwed.String(),
		"trackedPayers": len(payers),
	})

	if !solvent {
		log.Printf("[solvency] VIOLATION: FilecoinPay balance=%s < totalOwed=%s",
			filPayBalance, totalOwed)
	}
}

// checkProviderIDConsistency verifies that for every tracked dataset, the
// on-chain addressToProviderId(serviceProvider) matches the providerId from the event.
func checkProviderIDConsistency(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	if cfg.RegistryAddr == nil {
		return
	}

	datasets := state.GetDatasets()
	if len(datasets) == 0 {
		return
	}

	for _, ds := range datasets {
		calldata := foc.BuildCalldata(foc.SigAddrToProvId, foc.EncodeAddress(ds.ServiceProvider))
		result, err := foc.EthCallUint256(ctx, node, cfg.RegistryAddr, calldata)
		if err != nil {
			log.Printf("[provider-id] addressToProviderId(%x) call failed: %v", ds.ServiceProvider, err)
			continue
		}

		expected := bigIntFromUint64(ds.ProviderID)
		consistent := result.Cmp(expected) == 0

		assert.Always(consistent, "Provider ID matches registry for dataset", map[string]any{
			"dataSetId":          ds.DataSetID,
			"serviceProvider":    fmt.Sprintf("0x%x", ds.ServiceProvider),
			"expectedProviderId": ds.ProviderID,
			"actualProviderId":   result.Uint64(),
		})

		if !consistent {
			log.Printf("[provider-id] VIOLATION: addressToProviderId(%x) returned %s, expected %d",
				ds.ServiceProvider, result, ds.ProviderID)
		}
	}
}

// checkProofSetLiveness verifies that every tracked (non-deleted) dataset
// reports as live on-chain via PDPVerifier.dataSetLive().
func checkProofSetLiveness(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	if cfg.PDPAddr == nil {
		return
	}

	datasets := state.GetDatasets()
	for _, ds := range datasets {
		if ds.Deleted {
			continue
		}

		dsIDBytes := foc.EncodeBigInt(bigIntFromUint64(ds.DataSetID))
		live, err := foc.EthCallBool(ctx, node, cfg.PDPAddr, foc.BuildCalldata(foc.SigDataSetLive, dsIDBytes))
		if err != nil {
			log.Printf("[proofset-liveness] dataSetLive(%d) call failed: %v", ds.DataSetID, err)
			continue
		}

		assert.Always(live, "Active proofset is live on-chain", map[string]any{
			"dataSetId": ds.DataSetID,
			"live":      live,
		})

		if !live {
			log.Printf("[proofset-liveness] VIOLATION: dataset %d is tracked as active but dataSetLive=false", ds.DataSetID)
		}
	}
}

// checkDeletedDataSetNotLive verifies that deleted datasets are NOT live.
func checkDeletedDataSetNotLive(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	if cfg.PDPAddr == nil {
		return
	}

	datasets := state.GetDatasets()
	for _, ds := range datasets {
		if !ds.Deleted {
			continue
		}

		dsIDBytes := foc.EncodeBigInt(bigIntFromUint64(ds.DataSetID))
		live, err := foc.EthCallBool(ctx, node, cfg.PDPAddr, foc.BuildCalldata(foc.SigDataSetLive, dsIDBytes))
		if err != nil {
			continue
		}

		assert.Always(!live, "Deleted proofset is not live", map[string]any{
			"dataSetId": ds.DataSetID,
			"live":      live,
		})

		if live {
			log.Printf("[deleted-dataset] VIOLATION: dataset %d was deleted but dataSetLive=true", ds.DataSetID)
		}
	}
}

// checkProvingAdvancement verifies that proving is advancing for active datasets.
// It tracks getNextChallengeEpoch and getDataSetLastProvenEpoch over time.
// If the challenge epoch hasn't advanced for many consecutive polls while the chain
// is past the deadline, something is wrong with the proving pipeline.
func checkProvingAdvancement(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	if cfg.PDPAddr == nil {
		return
	}

	datasets := state.GetDatasets()
	for _, ds := range datasets {
		if ds.Deleted {
			continue
		}

		dsIDBytes := foc.EncodeBigInt(bigIntFromUint64(ds.DataSetID))

		challengeEpoch, err := foc.EthCallUint256(ctx, node, cfg.PDPAddr, foc.BuildCalldata(foc.SigGetNextChallengeEpoch, dsIDBytes))
		if err != nil {
			log.Printf("[proving-advancement] getNextChallengeEpoch(%d) failed: %v", ds.DataSetID, err)
			continue
		}

		provenEpoch, err := foc.EthCallUint256(ctx, node, cfg.PDPAddr, foc.BuildCalldata(foc.SigGetLastProvenEpoch, dsIDBytes))
		if err != nil {
			log.Printf("[proving-advancement] getDataSetLastProvenEpoch(%d) failed: %v", ds.DataSetID, err)
			continue
		}

		challengeAdv, provenAdv := state.UpdateProvingState(ds.DataSetID, challengeEpoch.Uint64(), provenEpoch.Uint64())

		if challengeAdv {
			assert.Sometimes(true, "Proving period advances (challenge epoch changed)", map[string]any{
				"dataSetId":      ds.DataSetID,
				"challengeEpoch": challengeEpoch.Uint64(),
			})
		}

		if provenAdv {
			assert.Sometimes(true, "Dataset proof submitted (proven epoch advanced)", map[string]any{
				"dataSetId":   ds.DataSetID,
				"provenEpoch": provenEpoch.Uint64(),
			})
			log.Printf("[proving-advancement] dataset %d proven epoch advanced to %s", ds.DataSetID, provenEpoch)
		}

		// Log periodic status
		updated := state.GetDatasets()
		for _, u := range updated {
			if u.DataSetID == ds.DataSetID {
				if u.ChallengeEpochStale > 0 && u.ChallengeEpochStale%25 == 0 {
					log.Printf("[proving-advancement] dataset %d challenge epoch stale for %d polls (challenge=%s proven=%s)",
						ds.DataSetID, u.ChallengeEpochStale, challengeEpoch, provenEpoch)
				}
				break
			}
		}
	}
}

// checkPieceAccountingConsistency verifies that activePieceCount <= leafCount
// for every tracked dataset. Active pieces can never exceed total leaves.
func checkPieceAccountingConsistency(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	if cfg.PDPAddr == nil {
		return
	}

	datasets := state.GetDatasets()
	for _, ds := range datasets {
		if ds.Deleted {
			continue
		}

		dsIDBytes := foc.EncodeBigInt(bigIntFromUint64(ds.DataSetID))

		activeCount, err := foc.EthCallUint256(ctx, node, cfg.PDPAddr, foc.BuildCalldata(foc.SigGetActivePieceCount, dsIDBytes))
		if err != nil {
			continue
		}

		leafCount, err := foc.EthCallUint256(ctx, node, cfg.PDPAddr, foc.BuildCalldata(foc.SigGetDataSetLeafCount, dsIDBytes))
		if err != nil {
			continue
		}

		consistent := activeCount.Cmp(leafCount) <= 0

		assert.Always(consistent, "Active piece count does not exceed leaf count", map[string]any{
			"dataSetId":    ds.DataSetID,
			"activePieces": activeCount.String(),
			"leafCount":    leafCount.String(),
		})

		if !consistent {
			log.Printf("[piece-accounting] VIOLATION: dataset %d activePieces=%s > leafCount=%s",
				ds.DataSetID, activeCount, leafCount)
		}
	}
}

// checkActivePieceCount queries active piece count for all tracked datasets
// and logs as a reachability assertion. (Note: EthCallUint256 uses SetBytes which
// always returns non-negative, so a negativity check would be tautological.)
func checkActivePieceCount(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	if cfg.PDPAddr == nil {
		return
	}

	datasets := state.GetDatasets()
	for _, ds := range datasets {
		if ds.Deleted {
			continue
		}

		dsIDBytes := foc.EncodeBigInt(bigIntFromUint64(ds.DataSetID))
		count, err := foc.EthCallUint256(ctx, node, cfg.PDPAddr, foc.BuildCalldata(foc.SigGetActivePieceCount, dsIDBytes))
		if err != nil {
			continue
		}

		assert.Reachable("Queried active piece count for dataset", map[string]any{
			"dataSetId":    ds.DataSetID,
			"activePieces": count.String(),
		})
	}
}

// checkRateConsistency verifies that active datasets with pieces have a
// non-zero payment rate on their PDP rail.
func checkRateConsistency(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	if cfg.PDPAddr == nil || cfg.FilPayAddr == nil {
		return
	}

	datasets := state.GetDatasets()
	for _, ds := range datasets {
		if ds.Deleted || ds.PDPRailID == 0 {
			continue
		}

		dsIDBytes := foc.EncodeBigInt(bigIntFromUint64(ds.DataSetID))
		activeCount, err := foc.EthCallUint256(ctx, node, cfg.PDPAddr, foc.BuildCalldata(foc.SigGetActivePieceCount, dsIDBytes))
		if err != nil || activeCount.Sign() == 0 {
			continue // no pieces yet, rate can legitimately be zero
		}

		rate := foc.ReadRailPaymentRate(ctx, node, cfg.FilPayAddr, ds.PDPRailID)
		if rate == nil {
			log.Printf("[rate-consistency] getRail(%d) failed for dataset %d", ds.PDPRailID, ds.DataSetID)
			continue
		}

		hasRate := rate.Sign() > 0

		assert.Always(hasRate, "Active dataset rail has non-zero payment rate", map[string]any{
			"dataSetId":    ds.DataSetID,
			"pdpRailId":    ds.PDPRailID,
			"activePieces": activeCount.String(),
			"paymentRate":  rate.String(),
		})

		if !hasRate {
			log.Printf("[rate-consistency] VIOLATION: dataset %d rail %d has activePieces=%s but paymentRate=0",
				ds.DataSetID, ds.PDPRailID, activeCount)
		}
	}
}

// checkPayerLockup verifies that payers with active (non-deleted) datasets
// have non-zero lockup in FilecoinPay.
func checkPayerLockup(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	if cfg.USDFCAddr == nil || cfg.FilPayAddr == nil {
		return
	}

	datasets := state.GetDatasets()
	payers := state.GetTrackedPayers()
	if len(payers) == 0 {
		return
	}

	// Build set of payers that have at least one active dataset
	activePayers := map[string]bool{}
	for _, ds := range datasets {
		if !ds.Deleted && len(ds.Payer) > 0 {
			activePayers[fmt.Sprintf("%x", ds.Payer)] = true
		}
	}

	for _, payer := range payers {
		key := fmt.Sprintf("%x", payer)
		if !activePayers[key] {
			continue
		}

		lockup := foc.ReadAccountLockup(ctx, node, cfg.FilPayAddr, cfg.USDFCAddr, payer)
		hasLockup := lockup.Sign() > 0

		assert.Always(hasLockup, "Payer with active datasets has non-zero lockup", map[string]any{
			"payer":  fmt.Sprintf("0x%x", payer),
			"lockup": lockup.String(),
		})

		if !hasLockup {
			log.Printf("[payer-lockup] VIOLATION: payer %x has active datasets but lockup=0", payer)
		}
	}
}
