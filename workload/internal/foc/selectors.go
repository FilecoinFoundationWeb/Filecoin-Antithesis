package foc

import "golang.org/x/crypto/sha3"

// CalcSelector returns the first 4 bytes of keccak256(funcSig).
func CalcSelector(funcSig string) []byte {
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write([]byte(funcSig))
	return hasher.Sum(nil)[:4]
}

// Function selectors — computed at init time.
var (
	// USDFC (ERC20)
	SigTotalSupply = CalcSelector("totalSupply()")
	SigBalanceOf   = CalcSelector("balanceOf(address)")
	SigTransfer    = CalcSelector("transfer(address,uint256)")
	SigApprove     = CalcSelector("approve(address,uint256)")

	// FilecoinPayV1
	SigAccounts        = CalcSelector("accounts(address,address)")
	SigDeposit         = CalcSelector("deposit(address,address,uint256)")
	SigSetOpApproval   = CalcSelector("setOperatorApproval(address,address,bool,uint256,uint256,uint256)")
	SigSettleRail      = CalcSelector("settleRail(uint256,uint256)")
	SigGetRailsByPayer = CalcSelector("getRailsForPayerAndToken(address,address,uint256,uint256)")
	SigWithdraw          = CalcSelector("withdraw(address,uint256)")
	SigOperatorApprovals = CalcSelector("operatorApprovals(address,address,address)")
	SigCreateRail      = CalcSelector("createRail(address,address,address,address,uint256,address)")
	SigModifyRailPayment = CalcSelector("modifyRailPayment(uint256,uint256,uint256)")

	// ServiceProviderRegistry
	SigAddrToProvId = CalcSelector("addressToProviderId(address)")

	// FilecoinWarmStorageService
	SigTerminateService = CalcSelector("terminateService(uint256)")
	SigRailToDataSet    = CalcSelector("railToDataSet(uint256)")

	// PDPVerifier
	SigCreateDataSet          = CalcSelector("createDataSet(address,bytes)")
	SigAddPieces              = CalcSelector("addPieces(uint256,address,bytes[],bytes)")
	SigSchedulePieceDeletions = CalcSelector("schedulePieceDeletions(uint256,uint256[],bytes)")
	SigDeleteDataSet          = CalcSelector("deleteDataSet(uint256,bytes)")
	SigGetActivePieceCount    = CalcSelector("getActivePieceCount(uint256)")
	SigDataSetLive            = CalcSelector("dataSetLive(uint256)")
	SigGetNextChallengeEpoch  = CalcSelector("getNextChallengeEpoch(uint256)")
	SigGetDataSetLeafCount    = CalcSelector("getDataSetLeafCount(uint256)")
)
