package resources

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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

// Helper functions for generating random data
func generateRandomBytes(min, max int) []byte {
	n := mathrand.Intn(max-min) + min
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func generateRandomString(min, max int) string {
	chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()_+-=[]{}|;:,.<>?"
	length := mathrand.Intn(max-min) + min
	result := make([]byte, length)
	for i := range result {
		result[i] = chars[mathrand.Intn(len(chars))]
	}
	return string(result)
}

func generateRandomBase64(min, max int) string {
	return base64.StdEncoding.EncodeToString(generateRandomBytes(min, max))
}

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

func generateRandomAddress() address.Address {
	if mathrand.Float32() < 0.3 {
		return address.Undef
	}
	addr, _ := address.NewIDAddress(uint64(mathrand.Int63()))
	return addr
}

// FuzzBlockSubmission generates and submits malformed blocks
func FuzzBlockSubmission(ctx context.Context, api api.FullNode) error {
	mathrand.Seed(time.Now().UnixNano())

	// Generate different types of malformed blocks
	testCases := []struct {
		name  string
		block *types.BlockMsg
	}{
		{
			name:  "EmptyBlock",
			block: &types.BlockMsg{},
		},
		{
			name: "NilHeader",
			block: &types.BlockMsg{
				Header: nil,
			},
		},
		{
			name: "MalformedMinerAddress",
			block: &types.BlockMsg{
				Header: &types.BlockHeader{
					Miner:  generateRandomAddress(),
					Height: abi.ChainEpoch(mathrand.Int63()),
				},
			},
		},
		{
			name: "InvalidVRFProof",
			block: &types.BlockMsg{
				Header: &types.BlockHeader{
					Miner: generateRandomAddress(),
					Ticket: &types.Ticket{
						VRFProof: []byte(generateRandomString(0, 1000)),
					},
				},
			},
		},
		{
			name: "OverflowValues",
			block: &types.BlockMsg{
				Header: &types.BlockHeader{
					Miner:         generateRandomAddress(),
					Height:        abi.ChainEpoch(math.MaxInt64),
					Timestamp:     uint64(math.MaxUint64),
					ForkSignaling: uint64(math.MaxUint64),
				},
			},
		},
		{
			name: "InvalidSignatures",
			block: &types.BlockMsg{
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
			},
		},
		{
			name: "InvalidCIDs",
			block: &types.BlockMsg{
				Header: &types.BlockHeader{
					Miner:                 generateRandomAddress(),
					ParentStateRoot:       generateRandomCID(),
					Messages:              generateRandomCID(),
					ParentMessageReceipts: generateRandomCID(),
				},
			},
		},
		{
			name: "InvalidBeaconEntries",
			block: &types.BlockMsg{
				Header: &types.BlockHeader{
					Miner: generateRandomAddress(),
					BeaconEntries: []types.BeaconEntry{
						{
							Round: uint64(mathrand.Int63()),
							Data:  generateRandomBytes(0, 1000),
						},
					},
				},
			},
		},
		{
			name: "InvalidWinPoStProof",
			block: &types.BlockMsg{
				Header: &types.BlockHeader{
					Miner: generateRandomAddress(),
					WinPoStProof: []proof.PoStProof{
						{
							PoStProof:  abi.RegisteredPoStProof(mathrand.Int31()),
							ProofBytes: generateRandomBytes(0, 1000),
						},
					},
				},
			},
		},
		{
			name: "EmptyArrays",
			block: &types.BlockMsg{
				Header: &types.BlockHeader{
					Miner:         generateRandomAddress(),
					Parents:       []cid.Cid{},
					BeaconEntries: []types.BeaconEntry{},
					WinPoStProof:  []proof.PoStProof{},
				},
				BlsMessages:   []cid.Cid{},
				SecpkMessages: []cid.Cid{},
			},
		},
		{
			name: "HugeArrays",
			block: &types.BlockMsg{
				Header: &types.BlockHeader{
					Miner:         generateRandomAddress(),
					Parents:       make([]cid.Cid, 1000),
					BeaconEntries: make([]types.BeaconEntry, 1000),
					WinPoStProof:  make([]proof.PoStProof, 1000),
				},
				BlsMessages:   make([]cid.Cid, 1000),
				SecpkMessages: make([]cid.Cid, 1000),
			},
		},
		{
			name: "LargeVRFProof",
			block: &types.BlockMsg{
				Header: &types.BlockHeader{
					Miner: generateRandomAddress(),
					Ticket: &types.Ticket{
						VRFProof: generateRandomBytes(1024*1024, 2*1024*1024), // 1-2MB of random data
					},
				},
			},
		},
		{
			name: "LargeBeaconData",
			block: &types.BlockMsg{
				Header: &types.BlockHeader{
					Miner: generateRandomAddress(),
					BeaconEntries: []types.BeaconEntry{
						{
							Round: uint64(mathrand.Int63()),
							Data:  generateRandomBytes(5*1024*1024, 10*1024*1024), // 5-10MB of random data
						},
					},
				},
			},
		},
		{
			name: "ManyBeaconEntries",
			block: &types.BlockMsg{
				Header: &types.BlockHeader{
					Miner: generateRandomAddress(),
					BeaconEntries: func() []types.BeaconEntry {
						entries := make([]types.BeaconEntry, 10000)
						for i := range entries {
							entries[i] = types.BeaconEntry{
								Round: uint64(i),
								Data:  generateRandomBytes(1000, 2000),
							}
						}
						return entries
					}(),
				},
			},
		},
		{
			name: "LargeSignature",
			block: &types.BlockMsg{
				Header: &types.BlockHeader{
					Miner: generateRandomAddress(),
					BlockSig: &crypto.Signature{
						Type: crypto.SigTypeBLS,
						Data: generateRandomBytes(10*1024*1024, 20*1024*1024), // 10-20MB signature
					},
				},
			},
		},
		{
			name: "LargeWinPoStProof",
			block: &types.BlockMsg{
				Header: &types.BlockHeader{
					Miner: generateRandomAddress(),
					WinPoStProof: []proof.PoStProof{
						{
							PoStProof:  abi.RegisteredPoStProof_StackedDrgWinning2KiBV1,
							ProofBytes: generateRandomBytes(50*1024*1024, 100*1024*1024), // 50-100MB proof
						},
					},
				},
			},
		},
	}

	// Create some valid PoSt proofs to use as base
	posts := []proof.PoStProof{
		{
			PoStProof:  abi.RegisteredPoStProof_StackedDrgWinning2KiBV1,
			ProofBytes: []byte{0x07},
		},
	}

	// Add some completely random blocks
	for i := 0; i < 20; i++ {
		testCases = append(testCases, struct {
			name  string
			block *types.BlockMsg
		}{
			name: "RandomBlock_" + generateRandomString(5, 10),
			block: &types.BlockMsg{
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
			},
		})
	}

	// Submit all test cases
	for _, tc := range testCases {
		log.Printf("[INFO] Submitting test case: %s", tc.name)
		err := api.SyncSubmitBlock(ctx, tc.block)

		if err != nil {
			log.Printf("[INFO] Test case %s: Block rejected as expected with error: %v", tc.name, err)
		} else {
			log.Printf("[WARN] Test case %s: Block unexpectedly accepted!", tc.name)
		}

		// The node should reject all these malformed blocks
		assert.Always(err != nil,
			"[Block Validation] Malformed block submission should be rejected",
			map[string]interface{}{
				"test_case": tc.name,
				"error":     err,
				"property":  "Block validation",
				"impact":    "Critical - validates block validation security",
				"details":   "Node must reject malformed blocks to maintain chain integrity",
			})

		// Add a small delay between test cases
		time.Sleep(100 * time.Millisecond)
	}

	log.Printf("[INFO] Completed %d test cases", len(testCases))
	return nil
}
