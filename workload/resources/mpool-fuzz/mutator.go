package mpoolfuzz

import (
	"math/rand"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
)

// MutationType represents the category of mutation to apply
type MutationType int

const (
	// Standard mutation types
	MutationGasPremiumFeeCap MutationType = iota
	MutationNegativeGasLimit
	MutationMaxValue
	MutationGarbageParams
	MutationReservedAddress
	MutationOversizedParams
	MutationUnsupportedVersion
	MutationHighGasLimit
	MutationEmptyParams
	MutationUnicodeParams
	MutationSelfReference
	MutationIntegerOverflow
	MutationMinimalGas
)

// Apply mutates a message according to the specified mutation type
func Apply(msg *types.Message, mutationType MutationType, r *rand.Rand) string {
	switch mutationType {
	// Standard mutations
	case MutationGasPremiumFeeCap:
		// Realistic gas values but premium > fee cap
		msg.GasPremium = abi.NewTokenAmount(5000000000) // 5 nanoFIL
		msg.GasFeeCap = abi.NewTokenAmount(1000000000)  // 1 nanoFIL
		return "GasPremium > GasFeeCap (realistic values)"

	case MutationNegativeGasLimit:
		msg.GasLimit = -50000
		return "Negative gas limit"

	case MutationMaxValue:
		// Use a more realistic but still excessive value
		msg.Value = abi.NewTokenAmount(1000000000000000000) // 1000 FIL (in attoFIL)
		return "Excessive value (1000 FIL)"

	case MutationGarbageParams:
		msg.Method = 99
		msg.Params = RandomBytes(256) // More realistic garbage size
		return "Garbage params for method 99"

	case MutationReservedAddress:
		// Note: Ignoring potential error here for simplicity
		msg.To, _ = address.NewIDAddress(0)
		return "Reserved ID address 0"

	case MutationOversizedParams:
		msg.Params = RandomBytes(8192) // 8KB - more realistic oversized
		return "Oversized params (8KB)"

	case MutationUnsupportedVersion:
		msg.Version = 2
		return "Unsupported message version"

	case MutationHighGasLimit:
		msg.GasLimit = 1000000000 // 1 billion - realistic but excessive
		return "Excessively high gas limit (1B)"

	case MutationEmptyParams:
		msg.Method = 5
		msg.Params = []byte{}
		return "Empty params with non-zero method"

	case MutationUnicodeParams:
		msg.Params = []byte("☢️💥🔥🚀💎")
		return "Unicode params"

	case MutationSelfReference:
		msg.To = msg.From
		msg.Method = 12
		return "Self-referential actor call"

	case MutationIntegerOverflow:
		// Use more realistic but still problematic values
		msg.GasFeeCap = abi.NewTokenAmount(9223372036854775807)  // Max int64
		msg.GasPremium = abi.NewTokenAmount(9223372036854775807) // Max int64
		return "Integer overflow gas parameters"

	case MutationMinimalGas:
		msg.GasFeeCap = abi.NewTokenAmount(100) // Too low
		msg.GasPremium = abi.NewTokenAmount(50) // Too low
		msg.GasLimit = 100                      // Too low
		return "Insufficient gas values"
	}

	return "Unknown mutation"
}

func GetRandomMutation(category string, r *rand.Rand) MutationType {
	switch category {
	case "standard":
		return MutationType(r.Intn(13))
	default:
		return MutationType(r.Intn(13)) // Only standard mutations
	}
}
