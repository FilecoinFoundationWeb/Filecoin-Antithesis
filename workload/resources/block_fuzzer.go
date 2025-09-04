package resources

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"math"
	mathrand "math/rand"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/proof"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
)

// generateRandomBytes generates a random byte slice of length between min and max
func generateRandomBytes(min, max int) []byte {
	n := mathrand.Intn(max-min) + min
	b := make([]byte, n)
	rand.Read(b)
	return b
}

// generateRandomString generates a random string of length between min and max
// using a mix of alphanumeric and special characters
func generateRandomString(min, max int) string {
	chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()_+-=[]{}|;:,.<>?"
	length := mathrand.Intn(max-min) + min
	result := make([]byte, length)
	for i := range result {
		result[i] = chars[mathrand.Intn(len(chars))]
	}
	return string(result)
}

// generateRandomBase64 generates a random base64-encoded string from random bytes
// of length between min and max
func generateRandomBase64(min, max int) string {
	return base64.StdEncoding.EncodeToString(generateRandomBytes(min, max))
}

// generateRandomCID generates either a valid or invalid CID with 50% probability
func generateRandomCID() cid.Cid {
	// Generate either valid or invalid CID
	if mathrand.Float32() < 0.5 {
		// Invalid CID
		return cid.Cid{}
	}
	// Valid but random CID
	b := generateRandomBytes(32, 64)
	pref := cid.Prefix{
		Version:  1,
		Codec:    cid.Raw,
		MhType:   0x12, // sha2-256
		MhLength: 32,
	}
	c, _ := pref.Sum(b)
	return c
}

// generateRandomAddress generates either an undefined address (30% probability)
// or a random ID address (70% probability)
func generateRandomAddress() address.Address {
	if mathrand.Float32() < 0.3 {
		return address.Undef
	}
	addr, _ := address.NewIDAddress(uint64(mathrand.Int63()))
	return addr
}

type BlockFuzzerConfig struct {
	EnableLargeData    bool
	EnableOverflow     bool
	EnableInvalidCIDs  bool
	EnableRandomBlocks bool
	MaxRandomBlocks    int
	DelayBetweenTests  time.Duration
}

// DefaultBlockFuzzerConfig returns a default configuration
func DefaultBlockFuzzerConfig() *BlockFuzzerConfig {
	return &BlockFuzzerConfig{
		EnableLargeData:    true,
		EnableOverflow:     true,
		EnableInvalidCIDs:  true,
		EnableRandomBlocks: true,
		MaxRandomBlocks:    20,
		DelayBetweenTests:  100 * time.Millisecond,
	}
}

// CreateEmptyBlock creates an empty block for testing
func CreateEmptyBlock() *types.BlockMsg {
	return &types.BlockMsg{}
}

// CreateNilHeaderBlock creates a block with nil header
func CreateNilHeaderBlock() *types.BlockMsg {
	return &types.BlockMsg{
		Header: nil,
	}
}

// CreateMalformedMinerAddressBlock creates a block with malformed miner address
func CreateMalformedMinerAddressBlock() *types.BlockMsg {
	return &types.BlockMsg{
		Header: &types.BlockHeader{
			Miner:  generateRandomAddress(),
			Height: abi.ChainEpoch(mathrand.Int63()),
		},
	}
}

// CreateInvalidVRFProofBlock creates a block with invalid VRF proof
func CreateInvalidVRFProofBlock() *types.BlockMsg {
	return &types.BlockMsg{
		Header: &types.BlockHeader{
			Miner: generateRandomAddress(),
			Ticket: &types.Ticket{
				VRFProof: []byte(generateRandomString(0, 1000)),
			},
		},
	}
}

// CreateOverflowValuesBlock creates a block with overflow values
func CreateOverflowValuesBlock() *types.BlockMsg {
	return &types.BlockMsg{
		Header: &types.BlockHeader{
			Miner:         generateRandomAddress(),
			Height:        abi.ChainEpoch(math.MaxInt64),
			Timestamp:     uint64(math.MaxUint64),
			ForkSignaling: uint64(math.MaxUint64),
		},
	}
}

// CreateInvalidSignaturesBlock creates a block with invalid signatures
func CreateInvalidSignaturesBlock() *types.BlockMsg {
	return &types.BlockMsg{
		Header: &types.BlockHeader{
			Miner: generateRandomAddress(),
			BlockSig: &crypto.Signature{
				Type: 99, // Invalid type
				Data: generateRandomBytes(0, 1000),
			},
			BLSAggregate: &crypto.Signature{
				Type: crypto.SigTypeBLS,
				Data: generateRandomBytes(0, 1000),
			},
		},
	}
}

// CreateInvalidCIDsBlock creates a block with invalid CIDs
func CreateInvalidCIDsBlock() *types.BlockMsg {
	return &types.BlockMsg{
		Header: &types.BlockHeader{
			Miner:                 generateRandomAddress(),
			ParentStateRoot:       generateRandomCID(),
			Messages:              generateRandomCID(),
			ParentMessageReceipts: generateRandomCID(),
		},
	}
}

// CreateInvalidBeaconEntriesBlock creates a block with invalid beacon entries
func CreateInvalidBeaconEntriesBlock() *types.BlockMsg {
	return &types.BlockMsg{
		Header: &types.BlockHeader{
			Miner: generateRandomAddress(),
			BeaconEntries: []types.BeaconEntry{
				{
					Round: uint64(mathrand.Int63()),
					Data:  generateRandomBytes(0, 1000),
				},
			},
		},
	}
}

// CreateInvalidWinPoStProofBlock creates a block with invalid WinPoSt proof
func CreateInvalidWinPoStProofBlock() *types.BlockMsg {
	return &types.BlockMsg{
		Header: &types.BlockHeader{
			Miner: generateRandomAddress(),
			WinPoStProof: []proof.PoStProof{
				{
					PoStProof:  abi.RegisteredPoStProof(mathrand.Int31()),
					ProofBytes: generateRandomBytes(0, 1000),
				},
			},
		},
	}
}

// CreateEmptyArraysBlock creates a block with empty arrays
func CreateEmptyArraysBlock() *types.BlockMsg {
	return &types.BlockMsg{
		Header: &types.BlockHeader{
			Miner:         generateRandomAddress(),
			Parents:       []cid.Cid{},
			BeaconEntries: []types.BeaconEntry{},
			WinPoStProof:  []proof.PoStProof{},
		},
		BlsMessages:   []cid.Cid{},
		SecpkMessages: []cid.Cid{},
	}
}

// CreateHugeArraysBlock creates a block with huge arrays
func CreateHugeArraysBlock() *types.BlockMsg {
	return &types.BlockMsg{
		Header: &types.BlockHeader{
			Miner:         generateRandomAddress(),
			Parents:       make([]cid.Cid, 1000),
			BeaconEntries: make([]types.BeaconEntry, 1000),
			WinPoStProof:  make([]proof.PoStProof, 1000),
		},
		BlsMessages:   make([]cid.Cid, 1000),
		SecpkMessages: make([]cid.Cid, 1000),
	}
}

// CreateLargeVRFProofBlock creates a block with large VRF proof
func CreateLargeVRFProofBlock() *types.BlockMsg {
	return &types.BlockMsg{
		Header: &types.BlockHeader{
			Miner: generateRandomAddress(),
			Ticket: &types.Ticket{
				VRFProof: generateRandomBytes(1024*1024, 2*1024*1024), // 1-2MB
			},
		},
	}
}

// CreateLargeBeaconDataBlock creates a block with large beacon data
func CreateLargeBeaconDataBlock() *types.BlockMsg {
	return &types.BlockMsg{
		Header: &types.BlockHeader{
			Miner: generateRandomAddress(),
			BeaconEntries: []types.BeaconEntry{
				{
					Round: uint64(mathrand.Int63()),
					Data:  generateRandomBytes(5*1024*1024, 10*1024*1024), // 5-10MB
				},
			},
		},
	}
}

// CreateManyBeaconEntriesBlock creates a block with many beacon entries
func CreateManyBeaconEntriesBlock() *types.BlockMsg {
	entries := make([]types.BeaconEntry, 10000)
	for i := range entries {
		entries[i] = types.BeaconEntry{
			Round: uint64(i),
			Data:  generateRandomBytes(1000, 2000),
		}
	}

	return &types.BlockMsg{
		Header: &types.BlockHeader{
			Miner:         generateRandomAddress(),
			BeaconEntries: entries,
		},
	}
}

// CreateLargeSignatureBlock creates a block with large signature
func CreateLargeSignatureBlock() *types.BlockMsg {
	return &types.BlockMsg{
		Header: &types.BlockHeader{
			Miner: generateRandomAddress(),
			BlockSig: &crypto.Signature{
				Type: crypto.SigTypeBLS,
				Data: generateRandomBytes(10*1024*1024, 20*1024*1024), // 10-20MB
			},
		},
	}
}

// CreateLargeWinPoStProofBlock creates a block with large WinPoSt proof
func CreateLargeWinPoStProofBlock() *types.BlockMsg {
	posts := []proof.PoStProof{
		{
			PoStProof:  abi.RegisteredPoStProof_StackedDrgWinning2KiBV1,
			ProofBytes: generateRandomBytes(50*1024*1024, 100*1024*1024), // 50-100MB
		},
	}

	return &types.BlockMsg{
		Header: &types.BlockHeader{
			Miner:        generateRandomAddress(),
			WinPoStProof: posts,
		},
	}
}

// CreateRandomBlock creates a completely random block
func CreateRandomBlock() *types.BlockMsg {
	posts := []proof.PoStProof{
		{
			PoStProof:  abi.RegisteredPoStProof_StackedDrgWinning2KiBV1,
			ProofBytes: []byte{0x07},
		},
	}

	return &types.BlockMsg{
		Header: &types.BlockHeader{
			Miner: generateRandomAddress(),
			Ticket: &types.Ticket{
				VRFProof: generateRandomBytes(0, 1000),
			},
			ElectionProof: &types.ElectionProof{
				WinCount: int64(mathrand.Int31()),
				VRFProof: generateRandomBytes(0, 1000),
			},
			BeaconEntries: []types.BeaconEntry{
				{
					Round: uint64(mathrand.Int63()),
					Data:  generateRandomBytes(0, 1000),
				},
			},
			WinPoStProof:          posts,
			Parents:               []cid.Cid{generateRandomCID()},
			ParentWeight:          types.NewInt(uint64(mathrand.Int63())),
			Height:                abi.ChainEpoch(mathrand.Int63()),
			ParentStateRoot:       generateRandomCID(),
			ParentMessageReceipts: generateRandomCID(),
			Messages:              generateRandomCID(),
			BLSAggregate: &crypto.Signature{
				Type: crypto.SigType(mathrand.Intn(3)),
				Data: generateRandomBytes(0, 1000),
			},
			Timestamp: uint64(mathrand.Int63()),
			BlockSig: &crypto.Signature{
				Type: crypto.SigType(mathrand.Intn(3)),
				Data: generateRandomBytes(0, 1000),
			},
			ForkSignaling: uint64(mathrand.Int31()),
			ParentBaseFee: abi.NewTokenAmount(mathrand.Int63()),
		},
		BlsMessages:   []cid.Cid{generateRandomCID()},
		SecpkMessages: []cid.Cid{generateRandomCID()},
	}
}

func GenerateMalformedBlocks(config *BlockFuzzerConfig) []*types.BlockMsg {
	var blocks []*types.BlockMsg

	blocks = append(blocks, CreateEmptyBlock())
	blocks = append(blocks, CreateNilHeaderBlock())
	blocks = append(blocks, CreateMalformedMinerAddressBlock())
	blocks = append(blocks, CreateInvalidVRFProofBlock())
	blocks = append(blocks, CreateInvalidSignaturesBlock())
	blocks = append(blocks, CreateInvalidCIDsBlock())
	blocks = append(blocks, CreateInvalidBeaconEntriesBlock())
	blocks = append(blocks, CreateInvalidWinPoStProofBlock())
	blocks = append(blocks, CreateEmptyArraysBlock())
	blocks = append(blocks, CreateHugeArraysBlock())

	if config.EnableOverflow {
		blocks = append(blocks, CreateOverflowValuesBlock())
	}

	if config.EnableLargeData {
		blocks = append(blocks, CreateLargeVRFProofBlock())
		blocks = append(blocks, CreateLargeBeaconDataBlock())
		blocks = append(blocks, CreateManyBeaconEntriesBlock())
		blocks = append(blocks, CreateLargeSignatureBlock())
		blocks = append(blocks, CreateLargeWinPoStProofBlock())
	}

	if config.EnableRandomBlocks {
		for i := 0; i < config.MaxRandomBlocks; i++ {
			blocks = append(blocks, CreateRandomBlock())
		}
	}

	return blocks
}

// FuzzBlockSubmission generates and submits various types of malformed blocks
func FuzzBlockSubmission(ctx context.Context, api api.FullNode) error {
	return FuzzBlockSubmissionWithConfig(ctx, api, DefaultBlockFuzzerConfig())
}

// FuzzBlockSubmissionWithConfig generates and submits malformed blocks with custom configuration
func FuzzBlockSubmissionWithConfig(ctx context.Context, api api.FullNode, config *BlockFuzzerConfig) error {
	mathrand.Seed(time.Now().UnixNano())

	// Generate malformed blocks based on configuration
	blocks := GenerateMalformedBlocks(config)

	// Submit all test cases
	for i, block := range blocks {
		testCaseName := fmt.Sprintf("TestCase_%d", i)
		log.Printf("[INFO] Submitting test case: %s", testCaseName)

		err := api.SyncSubmitBlock(ctx, block)

		if err != nil {
			log.Printf("[INFO] Test case %s: Block rejected as expected with error: %v", testCaseName, err)
		} else {
			log.Printf("[WARN] Test case %s: Block unexpectedly accepted!", testCaseName)
		}

		// The node should reject all these malformed blocks
		assert.Always(err != nil,
			"Block validation: Malformed block submission should be rejected - validation bypass detected",
			map[string]interface{}{
				"operation": "block_validation",
				"test_case": testCaseName,
				"error":     err,
				"property":  "Block validation",
				"impact":    "Critical - validates block validation security",
				"details":   "Node must reject malformed blocks to maintain chain integrity",
			})

		time.Sleep(config.DelayBetweenTests)
	}

	log.Printf("[INFO] Completed %d test cases", len(blocks))
	return nil
}

// PerformStressMaxMessageSize runs max message size stress test
func PerformStressMaxMessageSize(ctx context.Context, nodeConfig *NodeConfig) error {
	log.Printf("[INFO] Starting max message size stress test on node '%s'...", nodeConfig.Name)

	api, closer, err := ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return nil
	}
	defer closer()

	return RetryOperation(ctx, func() error {
		err := SendMaxSizedMessage(ctx, api)
		if err != nil {
			log.Printf("[ERROR] Max message size stress test failed: %v", err)
			return nil
		}

		log.Printf("[INFO] Max message size stress test completed successfully")
		return nil
	}, "Max message size stress test operation")
}

// PerformBlockFuzzing runs block fuzzing on a specified node
func PerformBlockFuzzing(ctx context.Context, nodeConfig *NodeConfig) error {
	log.Printf("[INFO] Starting block fuzzing on node '%s'...", nodeConfig.Name)

	api, closer, err := ConnectToNode(ctx, *nodeConfig)
	if err != nil {
		log.Printf("[ERROR] Failed to connect to Lotus node '%s': %v", nodeConfig.Name, err)
		return nil
	}
	defer closer()

	return RetryOperation(ctx, func() error {
		err := FuzzBlockSubmission(ctx, api)
		if err != nil {
			log.Printf("[WARN] Block fuzzing failed, will retry: %v", err)
			return err // Return original error for retry
		}

		log.Printf("[INFO] Block fuzzing completed successfully")
		return nil
	}, "Block fuzzing operation")
}
