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
