package mpoolfuzz

import (
	"math"
	"math/big"
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
		msg.GasPremium = abi.NewTokenAmount(100)
		msg.GasFeeCap = abi.NewTokenAmount(1)
		return "GasPremium > GasFeeCap"

	case MutationNegativeGasLimit:
		msg.GasLimit = -1000
		return "Negative gas limit"

	case MutationMaxValue:
		raw := new(big.Int).SetUint64(^uint64(0))
		msg.Value = abi.TokenAmount{Int: raw}
		return "Maximum uint64 value"

	case MutationGarbageParams:
		msg.Method = 99
		msg.Params = RandomBytes(128)
		return "Garbage params for method 99"

	case MutationReservedAddress:
		// Note: Ignoring potential error here for simplicity
		msg.To, _ = address.NewIDAddress(0)
		return "Reserved ID address 0"

	case MutationOversizedParams:
		msg.Params = RandomBytes(2048)
		return "Oversized params (2KB)"

	case MutationUnsupportedVersion:
		msg.Version = 2
		return "Unsupported message version"

	case MutationHighGasLimit:
		msg.GasLimit = 1 << 60
		return "Unrealistically high gas limit"

	case MutationEmptyParams:
		msg.Method = 5
		msg.Params = []byte{}
		return "Empty params with non-zero method"

	case MutationUnicodeParams:
		msg.Params = []byte("☢️💥🔥")
		return "Unicode params"

	case MutationSelfReference:
		msg.To = msg.From
		msg.Method = 12
		return "Self-referential actor call"

	case MutationIntegerOverflow:
		msg.GasFeeCap = abi.NewTokenAmount(math.MaxInt64)
		msg.GasPremium = abi.NewTokenAmount(math.MaxInt64)
		return "Integer overflow gas parameters"

	case MutationMinimalGas:
		msg.GasFeeCap = abi.NewTokenAmount(1)
		msg.GasPremium = abi.NewTokenAmount(1)
		msg.GasLimit = 1
		return "Minimal gas values"
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
