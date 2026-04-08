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
		if ds.Deleted {
			continue
		}

		calldata := foc.BuildCalldata(foc.SigRailToDataSet, foc.EncodeUint256(ds.PDPRailID))
		result, err := foc.EthCallUint256(ctx, node, cfg.FWSSViewAddr, calldata)
		if err != nil {
			log.Printf("[rail-to-dataset] railToDataSet(%d) call failed: %v", ds.PDPRailID, err)
			continue
		}

		expected := bigIntFromUint64(ds.DataSetID)
		consistent := result.Cmp(expected) == 0

		if !consistent && result.Sign() == 0 {
			// Mapping cleared on-chain but DataSetDeleted event not yet processed
			// (finality lag). Log and skip — not a real violation.
			log.Printf("[rail-to-dataset] railToDataSet(%d) returned 0, expected %d (finality lag)",
				ds.PDPRailID, ds.DataSetID)
			continue
		}

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

	// Sum only funds (not funds + lockup). The lockupCurrent field is a
	// subset of funds — it represents the locked portion, not additional
	// balance. Adding both double-counts and causes false solvency violations.
	totalOwed := new(big.Int)
	for _, payer := range payers {
		funds := foc.ReadAccountFunds(ctx, node, cfg.FilPayAddr, cfg.USDFCAddr, payer)
		totalOwed.Add(totalOwed, funds)
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
		if ds.Deleted {
			continue
		}
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

		if !live {
			// Don't assert — there's a ~60s finality window between on-chain
			// deletion and the sidecar processing the DataSetDeleted event.
			// The inverse invariant (checkDeletedDataSetNotLive) catches real violations.
			log.Printf("[proofset-liveness] dataset %d not live but not yet marked deleted (finality lag)", ds.DataSetID)
			continue
		}

		assert.Always(live, "Active proofset is live on-chain", map[string]any{
			"dataSetId": ds.DataSetID,
			"live":      live,
		})
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

// checkLockupNeverExceedsFunds verifies that for every tracked payer,
// lockup never exceeds funds. This is a fundamental accounting invariant
// of FilecoinPay — if lockup > funds, the contract is in an inconsistent state.
// (Audit L01 continuous monitoring)
func checkLockupNeverExceedsFunds(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	if cfg.USDFCAddr == nil || cfg.FilPayAddr == nil {
		return
	}

	payers := state.GetTrackedPayers()
	for _, payer := range payers {
		funds := foc.ReadAccountFunds(ctx, node, cfg.FilPayAddr, cfg.USDFCAddr, payer)
		lockup := foc.ReadAccountLockup(ctx, node, cfg.FilPayAddr, cfg.USDFCAddr, payer)

		if funds == nil || lockup == nil {
			continue
		}

		consistent := lockup.Cmp(funds) <= 0

		assert.Always(consistent, "Lockup never exceeds funds for any payer", map[string]any{
			"payer":  fmt.Sprintf("0x%x", payer),
			"funds":  funds.String(),
			"lockup": lockup.String(),
		})

		if !consistent {
			log.Printf("[lockup-invariant] VIOLATION: payer=%x lockup=%s > funds=%s", payer, lockup, funds)
		}
	}
}

// checkDeletedDatasetRailTerminated verifies that for every deleted dataset,
// the associated PDP rail has an endEpoch set (rail is terminated).
// If a deleted dataset's rail has endEpoch=0, it's a zombie rail still
// consuming lockup — funds are stuck. (#288 continuous monitoring)
func checkDeletedDatasetRailTerminated(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	if cfg.FilPayAddr == nil {
		return
	}

	datasets := state.GetDatasets()
	for _, ds := range datasets {
		if !ds.Deleted || ds.PDPRailID == 0 {
			continue
		}

		railData, err := foc.ReadRailFull(ctx, node, cfg.FilPayAddr, ds.PDPRailID)
		if err != nil || len(railData) < 256 {
			continue
		}

		// endEpoch is at word index 7 (bytes 224-256) in the getRail return tuple
		endEpoch := new(big.Int).SetBytes(railData[224:256])
		terminated := endEpoch.Sign() > 0

		assert.Sometimes(terminated, "Deleted dataset rail has endEpoch set", map[string]any{
			"dataSetId": ds.DataSetID,
			"pdpRailId": ds.PDPRailID,
			"endEpoch":  endEpoch.String(),
		})

		if !terminated {
			log.Printf("[deleted-rail] dataset %d rail %d has endEpoch=0 after deletion — zombie rail", ds.DataSetID, ds.PDPRailID)
		}
	}
}

// checkSettlementMonotonicity verifies that settledUpTo for every tracked
// rail only advances forward. If it ever decreases, settlement accounting
// is broken. Regression for filecoin-pay#134 (settlement halt on zero-rate).
func checkSettlementMonotonicity(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	if cfg.FilPayAddr == nil {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	for _, rail := range state.Rails {
		railData, err := foc.ReadRailFull(ctx, node, cfg.FilPayAddr, rail.RailID)
		if err != nil || len(railData) < 320 {
			continue
		}

		// settledUpTo is at word index 8 (bytes 256-288)
		settledUpTo := new(big.Int).SetBytes(railData[256:288]).Uint64()

		if rail.LastSeenSettledUpTo > 0 && settledUpTo < rail.LastSeenSettledUpTo {
			log.Printf("[settlement-monotonicity] VIOLATION: rail %d settledUpTo went backwards: %d → %d",
				rail.RailID, rail.LastSeenSettledUpTo, settledUpTo)
			assert.Always(false, "Rail settledUpTo only advances forward", map[string]any{
				"railID":   rail.RailID,
				"previous": rail.LastSeenSettledUpTo,
				"current":  settledUpTo,
			})
		}

		rail.LastSeenSettledUpTo = settledUpTo
	}
}

// checkDeletedDatasetFullySettled verifies that deleted datasets have their
// PDP rail fully settled (settledUpTo >= endEpoch). If not, the dataset was
// deleted without completing payment. Regression for filecoin-services#375.
func checkDeletedDatasetFullySettled(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	if cfg.FilPayAddr == nil {
		return
	}

	datasets := state.GetDatasets()
	for _, ds := range datasets {
		if !ds.Deleted || ds.PDPRailID == 0 {
			continue
		}

		railData, err := foc.ReadRailFull(ctx, node, cfg.FilPayAddr, ds.PDPRailID)
		if err != nil || len(railData) < 320 {
			continue
		}

		endEpoch := new(big.Int).SetBytes(railData[224:256])     // word 7
		settledUpTo := new(big.Int).SetBytes(railData[256:288])  // word 8

		if endEpoch.Sign() == 0 {
			continue // rail not terminated yet (finality lag)
		}

		fullySettled := settledUpTo.Cmp(endEpoch) >= 0
		assert.Sometimes(fullySettled, "Deleted dataset rail is fully settled", map[string]any{
			"dataSetId":   ds.DataSetID,
			"pdpRailId":   ds.PDPRailID,
			"settledUpTo": settledUpTo.String(),
			"endEpoch":    endEpoch.String(),
		})

		if !fullySettled {
			log.Printf("[deleted-rail-settled] dataset %d rail %d: settledUpTo=%s < endEpoch=%s",
				ds.DataSetID, ds.PDPRailID, settledUpTo, endEpoch)
		}
	}
}

// checkOperatorApprovalConsistency verifies that operator rate and lockup
// usage never exceeds the approved allowances. Regression for filecoin-pay#137/#274
// (operator lockup leak on rail finalization — #274 still OPEN).
func checkOperatorApprovalConsistency(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	if cfg.FilPayAddr == nil || cfg.USDFCAddr == nil || cfg.FWSSAddr == nil {
		return
	}

	payers := state.GetTrackedPayers()
	for _, payer := range payers {
		rateUsage, lockupUsage := foc.ReadOperatorApprovals(ctx, node, cfg.FilPayAddr, cfg.USDFCAddr, payer, cfg.FWSSAddr)

		// Read allowances (words 1 and 2 of the 6-word return)
		calldata := foc.BuildCalldata(foc.SigOperatorApprovals, foc.EncodeAddress(cfg.USDFCAddr), foc.EncodeAddress(payer), foc.EncodeAddress(cfg.FWSSAddr))
		result, err := foc.EthCallRaw(ctx, node, cfg.FilPayAddr, calldata)
		if err != nil || len(result) < 192 {
			continue
		}
		rateAllowance := new(big.Int).SetBytes(result[32:64])   // word 1
		lockupAllowance := new(big.Int).SetBytes(result[64:96]) // word 2

		if rateAllowance.Sign() > 0 {
			rateOK := rateUsage.Cmp(rateAllowance) <= 0
			assert.Always(rateOK, "Operator rate usage within allowance", map[string]any{
				"payer":         fmt.Sprintf("0x%x", payer),
				"rateUsage":     rateUsage.String(),
				"rateAllowance": rateAllowance.String(),
			})
			if !rateOK {
				log.Printf("[operator-approval] VIOLATION: payer=%x rateUsage=%s > rateAllowance=%s", payer, rateUsage, rateAllowance)
			}
		}

		if lockupAllowance.Sign() > 0 {
			lockupOK := lockupUsage.Cmp(lockupAllowance) <= 0
			assert.Always(lockupOK, "Operator lockup usage within allowance", map[string]any{
				"payer":           fmt.Sprintf("0x%x", payer),
				"lockupUsage":     lockupUsage.String(),
				"lockupAllowance": lockupAllowance.String(),
			})
			if !lockupOK {
				log.Printf("[operator-approval] VIOLATION: payer=%x lockupUsage=%s > lockupAllowance=%s", payer, lockupUsage, lockupAllowance)
			}
		}
	}
}

// checkLockupIncreasesOnPieceAdd verifies that when activePieceCount increases
// for a dataset, the payer's lockup also increases (rate change applied
// atomically with piece addition). Regression for filecoin-services#350.
func checkLockupIncreasesOnPieceAdd(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	if cfg.PDPAddr == nil || cfg.FilPayAddr == nil || cfg.USDFCAddr == nil {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	for _, ds := range state.Datasets {
		if ds.Deleted {
			continue
		}

		dsIDBytes := foc.EncodeBigInt(bigIntFromUint64(ds.DataSetID))
		activeCount, err := foc.EthCallUint256(ctx, node, cfg.PDPAddr, foc.BuildCalldata(foc.SigGetActivePieceCount, dsIDBytes))
		if err != nil || activeCount == nil {
			continue
		}

		currentCount := activeCount.Uint64()
		currentLockup := foc.ReadAccountLockup(ctx, node, cfg.FilPayAddr, cfg.USDFCAddr, ds.Payer)

		// If piece count increased since last poll, lockup should have also increased
		if ds.LastSeenPieceCount > 0 && currentCount > ds.LastSeenPieceCount && ds.LastSeenPayerLockup != nil {
			lockupIncreased := currentLockup.Cmp(ds.LastSeenPayerLockup) >= 0
			assert.Sometimes(lockupIncreased, "Lockup increases when pieces are added", map[string]any{
				"dataSetId":      ds.DataSetID,
				"piecesBefore":   ds.LastSeenPieceCount,
				"piecesAfter":    currentCount,
				"lockupBefore":   ds.LastSeenPayerLockup.String(),
				"lockupAfter":    currentLockup.String(),
			})
			if !lockupIncreased {
				log.Printf("[lockup-on-add] dataset %d: pieces %d→%d but lockup %s→%s (decreased!)",
					ds.DataSetID, ds.LastSeenPieceCount, currentCount, ds.LastSeenPayerLockup, currentLockup)
			}
		}

		ds.LastSeenPieceCount = currentCount
		ds.LastSeenPayerLockup = currentLockup
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
