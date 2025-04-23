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
	MutationMalformedCBOR
	MutationCBORJunk
	MutationUnsupportedVersion
	MutationHighGasLimit
	MutationEmptyParams
	MutationUnicodeParams
	MutationSelfReference
	MutationIntegerOverflow
	MutationMinimalCBOR
	MutationMinimalGas

	// Edge case mutation types
	EdgeCaseZeroGasLimit
	EdgeCaseNegativeValue
	EdgeCaseMethodOverflow
	EdgeCaseEnormousParams
	EdgeCaseZeroNonce

	// Subtle mutation types
	SubtleNonEmptyParams
	SubtleMinimalGasLimit
	SubtleExcessiveValue
	SubtleActorMethod
	SubtleMinimalValue
	SubtlePlausibleCBOR
	SubtleExcessiveGas
	SubtleSelfTransfer
	SubtleWrongMethod
	SubtleExceedsBalance
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

	case MutationMalformedCBOR:
		msg.Params = []byte{0xff, 0x01, 0x02, 0x03}
		return "Malformed CBOR"

	case MutationCBORJunk:
		msg.Params = []byte{0xa1, 0x63, 0x6a, 0x75, 0x6e, 0x6b, 0x58, 0x20}
		return "CBOR junk"

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

	case MutationMinimalCBOR:
		msg.Params = []byte{0x80}
		msg.Method = 6
		return "Minimal valid CBOR with unexpected method"

	case MutationMinimalGas:
		msg.GasFeeCap = abi.NewTokenAmount(1)
		msg.GasPremium = abi.NewTokenAmount(1)
		msg.GasLimit = 1
		return "Minimal gas values"

	// Edge cases
	case EdgeCaseZeroGasLimit:
		msg.GasLimit = 0
		return "Zero gas limit"

	case EdgeCaseNegativeValue:
		negative := big.NewInt(-1)
		msg.Value = abi.TokenAmount{Int: negative}
		return "Negative value"

	case EdgeCaseMethodOverflow:
		msg.Method = 1<<32 - 1
		return "Method number overflow"

	case EdgeCaseEnormousParams:
		msg.Params = make([]byte, 1024*1024)
		rand.Read(msg.Params)
		return "Enormous params (1MB)"

	case EdgeCaseZeroNonce:
		msg.Nonce = 0
		msg.Method = 0
		msg.Params = []byte{}
		msg.GasLimit = 1000000
		return "Zero nonce valid message"

	// Subtle mutations
	case SubtleNonEmptyParams:
		msg.Params = []byte{0x00}
		return "Transfer with non-empty params"

	case SubtleMinimalGasLimit:
		msg.GasLimit = 21000
		return "Minimal gas limit"

	case SubtleExcessiveValue:
		msg.Value = abi.NewTokenAmount(1000000000000001)
		return "Value greater than balance"

	case SubtleActorMethod:
		return "Actual actor with method 1"

	case SubtleMinimalValue:
		msg.Value = abi.NewTokenAmount(1)
		return "Minimal value transfer"

	case SubtlePlausibleCBOR:
		msg.Method = 2
		msg.Params = []byte{0xa1, 0x61, 0x01, 0x01}
		return "Plausible-looking but invalid CBOR"

	case SubtleExcessiveGas:
		msg.GasFeeCap = abi.NewTokenAmount(1e12)
		msg.GasPremium = abi.NewTokenAmount(1e11)
		return "Excessive gas price"

	case SubtleSelfTransfer:
		msg.To = msg.From
		return "Self-transfer"

	case SubtleWrongMethod:
		msg.Method = 3
		msg.Params = []byte{0x80}
		return "Valid params for wrong method"

	case SubtleExceedsBalance:
		msg.Value = abi.NewTokenAmount(1000000000000001)
		return "Value exceeds balance"
	}

	return "Unknown mutation"
}

func GetRandomMutation(category string, r *rand.Rand) MutationType {
	switch category {
	case "standard":
		return MutationType(r.Intn(16))
	case "edge":
		return MutationType(16 + r.Intn(5))
	case "subtle":
		return MutationType(21 + r.Intn(10))
	default:
		return MutationType(r.Intn(31))
	}
}
