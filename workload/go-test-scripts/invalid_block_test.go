package main

import (
	"fmt"
	"testing"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
)

func mockTipSet(t *testing.T) *types.TipSet {
	minerAct, err := address.NewIDAddress(0)
	assert.Always(err == nil, "Workload: Loading the resources config", map[string]interface{}{"error": err})
	c, err := cid.Decode("QmbFMke1KXqnYyBBWxB74N4c5SBnJMVAiMNRcGu6x1AwQH")
	assert.Always(err == nil, "Workload: Loading the resources config", map[string]interface{}{"error": err})

	blks := []*types.BlockHeader{
		{
			Miner:                 minerAct,
			Height:                abi.ChainEpoch(1),
			ParentStateRoot:       c,
			ParentMessageReceipts: c,
			Messages:              c,
			Ticket:                &types.Ticket{VRFProof: []byte{}},
		},
		{
			Miner:                 minerAct,
			Height:                abi.ChainEpoch(1),
			ParentStateRoot:       c,
			ParentMessageReceipts: c,
			Messages:              c,
			Ticket:                &types.Ticket{VRFProof: []byte{}},
		},
	}
	ts, err := types.NewTipSet(blks)
	fmt.Println(ts)
	assert.Always(err == nil, "Workload: Loading the resources config", map[string]interface{}{"error": err})
	return ts
}
