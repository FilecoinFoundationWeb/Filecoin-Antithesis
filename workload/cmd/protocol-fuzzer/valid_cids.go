package main

import (
	"context"
	"log"

	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/types"
	blockadt "github.com/filecoin-project/specs-actors/v7/actors/util/adt"
	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
)

// Deterministic CIDs for valid empty block construction.
// These must match what Lotus computes internally so that blocks pass
// checkMsgMeta() validation and get persisted to the blockstore.
var (
	emptyAMTCID     cid.Cid // Empty AMT (Array Mapped Trie) root
	emptyMsgMetaCID cid.Cid // MsgMeta{BlsMessages: emptyAMT, SecpkMessages: emptyAMT}
)

func init() {
	ctx := context.Background()
	cst := cbor.NewCborStore(blockstore.NewMemory())
	store := blockadt.WrapStore(ctx, cst)

	arr, err := blockadt.MakeEmptyArray(store, 3) // bitwidth=3 is the Filecoin default
	if err != nil {
		log.Fatalf("[protocol-fuzzer] FATAL: cannot create empty AMT: %v", err)
	}
	ac, err := arr.Root()
	if err != nil {
		log.Fatalf("[protocol-fuzzer] FATAL: cannot compute empty AMT CID: %v", err)
	}
	emptyAMTCID = ac

	mc, err := cst.Put(ctx, &types.MsgMeta{
		BlsMessages:   emptyAMTCID,
		SecpkMessages: emptyAMTCID,
	})
	if err != nil {
		log.Fatalf("[protocol-fuzzer] FATAL: cannot compute empty MsgMeta CID: %v", err)
	}
	emptyMsgMetaCID = mc
}
