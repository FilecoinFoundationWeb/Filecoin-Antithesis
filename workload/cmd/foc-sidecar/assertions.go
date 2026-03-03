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

// checkS03RailToDataset verifies that for every tracked dataset, the on-chain
// railToDataSet(pdpRailId) returns the correct dataSetId.
// Assertion S-03 [Always]: Rail-to-dataset reverse mapping consistency.
func checkS03RailToDataset(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	datasets := state.GetDatasets()
	if len(datasets) == 0 {
		return
	}

	for _, ds := range datasets {
		// eth_call: FWSS.railToDataSet(pdpRailId) -> uint256
		calldata := append(append([]byte{}, foc.SigRailToDataSet...),
			foc.EncodeUint256(ds.PDPRailID)...,
		)
		result, err := foc.EthCallUint256(ctx, node, cfg.FWSSAddr, calldata)
		if err != nil {
			log.Printf("[S-03] railToDataSet(%d) call failed: %v", ds.PDPRailID, err)
			continue
		}

		expected := bigIntFromUint64(ds.DataSetID)
		consistent := result.Cmp(expected) == 0

		assert.Always(consistent, "S-03: Rail-to-dataset reverse mapping is consistent", map[string]any{
			"pdpRailId":      ds.PDPRailID,
			"expectedDataSet": ds.DataSetID,
			"actualDataSet":  result.Uint64(),
		})

		if !consistent {
			log.Printf("[S-03] VIOLATION: railToDataSet(%d) returned %s, expected %d",
				ds.PDPRailID, result, ds.DataSetID)
		}
	}
}

// checkS05FilecoinPaySolvency verifies that the FilecoinPay contract holds at
// least as much USDFC as the sum of all tracked accounts' funds + lockup.
// Assertion S-05 [Always]: FilecoinPay solvency.
func checkS05FilecoinPaySolvency(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	if cfg.USDFCAddr == nil || cfg.FilPayAddr == nil {
		return
	}

	payers := state.GetTrackedPayers()
	if len(payers) == 0 {
		return
	}

	// Read USDFC.balanceOf(FilecoinPay)
	balCalldata := append(append([]byte{}, foc.SigBalanceOf...),
		foc.EncodeAddress(cfg.FilPayAddr)...,
	)
	filPayBalance, err := foc.EthCallUint256(ctx, node, cfg.USDFCAddr, balCalldata)
	if err != nil {
		log.Printf("[S-05] balanceOf(FilecoinPay) failed: %v", err)
		return
	}

	// Sum funds + lockup for all tracked payers
	totalOwed := new(big.Int)
	for _, payer := range payers {
		funds := foc.ReadAccountFunds(ctx, node, cfg.FilPayAddr, cfg.USDFCAddr, payer)
		lockup := foc.ReadAccountLockup(ctx, node, cfg.FilPayAddr, cfg.USDFCAddr, payer)
		totalOwed.Add(totalOwed, funds)
		totalOwed.Add(totalOwed, lockup)
	}

	solvent := filPayBalance.Cmp(totalOwed) >= 0

	assert.Always(solvent, "S-05: FilecoinPay holds sufficient USDFC (solvency)", map[string]any{
		"filPayBalance": filPayBalance.String(),
		"totalOwed":     totalOwed.String(),
		"trackedPayers": len(payers),
	})

	if !solvent {
		log.Printf("[S-05] VIOLATION: FilecoinPay balance=%s < totalOwed=%s",
			filPayBalance, totalOwed)
	}
}

// checkS10ProviderIDConsistency verifies that for every tracked dataset, the
// on-chain addressToProviderId(serviceProvider) matches the providerId from the event.
// Assertion S-10 [Always]: Provider ID consistency.
func checkS10ProviderIDConsistency(ctx context.Context, node api.FullNode, cfg *foc.Config, state *SidecarState) {
	if cfg.RegistryAddr == nil {
		return
	}

	datasets := state.GetDatasets()
	if len(datasets) == 0 {
		return
	}

	for _, ds := range datasets {
		// eth_call: ServiceProviderRegistry.addressToProviderId(serviceProvider) -> uint256
		calldata := append(append([]byte{}, foc.SigAddrToProvId...),
			foc.EncodeAddress(ds.ServiceProvider)...,
		)
		result, err := foc.EthCallUint256(ctx, node, cfg.RegistryAddr, calldata)
		if err != nil {
			log.Printf("[S-10] addressToProviderId(%x) call failed: %v", ds.ServiceProvider, err)
			continue
		}

		expected := bigIntFromUint64(ds.ProviderID)
		consistent := result.Cmp(expected) == 0

		assert.Always(consistent, "S-10: Provider ID matches registry for dataset", map[string]any{
			"dataSetId":          ds.DataSetID,
			"serviceProvider":    fmt.Sprintf("0x%x", ds.ServiceProvider),
			"expectedProviderId": ds.ProviderID,
			"actualProviderId":   result.Uint64(),
		})

		if !consistent {
			log.Printf("[S-10] VIOLATION: addressToProviderId(%x) returned %s, expected %d",
				ds.ServiceProvider, result, ds.ProviderID)
		}
	}
}
