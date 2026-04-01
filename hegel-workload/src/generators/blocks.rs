use cid::Cid;
use fvm_ipld_encoding::to_vec;
use fvm_shared::address::Address;
use fvm_shared::crypto::signature::Signature;
use fvm_shared::econ::TokenAmount;
use hegel::generators as gs;
use multihash_codetable::{Code, MultihashDigest};
use serde::ser::{SerializeSeq, Serializer};
use serde::Serialize;

use crate::cbor::{random_bytes};
use crate::generators::address::filecoin_address;

// ---------------------------------------------------------------------------
// Wire types — mirror Lotus's CBOR-generated encoding exactly.
// All are serialized as fixed-length CBOR arrays via custom Serialize impls.
// ---------------------------------------------------------------------------

/// Ticket: CBOR array(1) [VRFProof bytes] or CBOR null.
#[derive(Debug)]
struct Ticket {
    vrf_proof: Vec<u8>,
}

impl Serialize for Ticket {
    fn serialize<S: Serializer>(&self, s: S) -> Result<S::Ok, S::Error> {
        let mut seq = s.serialize_seq(Some(1))?;
        seq.serialize_element(&serde_bytes::Bytes::new(&self.vrf_proof))?;
        seq.end()
    }
}

/// ElectionProof: CBOR array(2) [WinCount int64, VRFProof bytes] or CBOR null.
#[derive(Debug)]
struct ElectionProof {
    win_count: i64,
    vrf_proof: Vec<u8>,
}

impl Serialize for ElectionProof {
    fn serialize<S: Serializer>(&self, s: S) -> Result<S::Ok, S::Error> {
        let mut seq = s.serialize_seq(Some(2))?;
        seq.serialize_element(&self.win_count)?;
        seq.serialize_element(&serde_bytes::Bytes::new(&self.vrf_proof))?;
        seq.end()
    }
}

/// Generate a random CID (CIDv1, dag-cbor codec, sha2-256 hash).
fn gen_random_cid() -> Cid {
    let data = random_bytes(32);
    let mh = Code::Sha2_256.digest(&data);
    Cid::new_v1(0x71, mh) // 0x71 = dag-cbor codec
}

/// BlockHeader: CBOR array(16) matching Lotus's field order exactly.
/// Pointer fields (Ticket, ElectionProof, BLSAggregate, BlockSig) are
/// Option<T> — None serializes as CBOR null, exercising nil-pointer paths.
#[derive(Debug)]
struct BlockHeader {
    miner: Address,
    ticket: Option<Ticket>,
    election_proof: Option<ElectionProof>,
    beacon_entries: Vec<()>, // empty slice → CBOR array(0)
    win_post_proof: Vec<()>, // empty slice → CBOR array(0)
    parents: Vec<Cid>,
    parent_weight: TokenAmount,
    height: i64,
    parent_state_root: Cid,
    parent_message_receipts: Cid,
    messages: Cid,
    bls_aggregate: Option<Signature>,
    timestamp: u64,
    block_sig: Option<Signature>,
    fork_signaling: u64,
    parent_base_fee: TokenAmount,
}

impl Serialize for BlockHeader {
    fn serialize<S: Serializer>(&self, s: S) -> Result<S::Ok, S::Error> {
        let mut seq = s.serialize_seq(Some(16))?;
        seq.serialize_element(&self.miner)?;
        seq.serialize_element(&self.ticket)?;
        seq.serialize_element(&self.election_proof)?;
        seq.serialize_element(&self.beacon_entries)?;
        seq.serialize_element(&self.win_post_proof)?;
        seq.serialize_element(&self.parents)?;
        seq.serialize_element(&self.parent_weight)?;
        seq.serialize_element(&self.height)?;
        seq.serialize_element(&self.parent_state_root)?;
        seq.serialize_element(&self.parent_message_receipts)?;
        seq.serialize_element(&self.messages)?;
        seq.serialize_element(&self.bls_aggregate)?;
        seq.serialize_element(&self.timestamp)?;
        seq.serialize_element(&self.block_sig)?;
        seq.serialize_element(&self.fork_signaling)?;
        seq.serialize_element(&self.parent_base_fee)?;
        seq.end()
    }
}

/// BlockMsg: CBOR array(3) [Header, BlsMessages []CID, SecpkMessages []CID].
#[derive(Debug)]
struct BlockMsg {
    header: BlockHeader,
    bls_messages: Vec<Cid>,
    secpk_messages: Vec<Cid>,
}

impl Serialize for BlockMsg {
    fn serialize<S: Serializer>(&self, s: S) -> Result<S::Ok, S::Error> {
        let mut seq = s.serialize_seq(Some(3))?;
        seq.serialize_element(&self.header)?;
        seq.serialize_element(&self.bls_messages)?;
        seq.serialize_element(&self.secpk_messages)?;
        seq.end()
    }
}

// ---------------------------------------------------------------------------
// Generators
// ---------------------------------------------------------------------------

/// Generate a `BlockMsg` as DAG-CBOR bytes.
#[hegel::composite]
pub fn block_msg(tc: hegel::TestCase) -> Vec<u8> {
    let header = tc.draw(gen_block_header());
    let bls_messages = tc.draw(gen_cid_list());
    let secpk_messages = tc.draw(gen_cid_list());
    let msg = BlockMsg {
        header,
        bls_messages,
        secpk_messages,
    };
    to_vec(&msg).expect("DAG-CBOR serialization of BlockMsg should not fail")
}

/// Generate a BlockHeader with fuzzed fields.
/// Pointer fields randomly alternate between Some/None to exercise nil-pointer
/// code paths — this is where many Filecoin bugs have been found.
#[hegel::composite]
fn gen_block_header(tc: hegel::TestCase) -> BlockHeader {
    let miner: Address = tc.draw(filecoin_address());

    // Ticket — nullable pointer field
    let has_ticket: bool = tc.draw(gs::booleans());
    let ticket = if has_ticket {
        let vrf_proof: Vec<u8> =
            tc.draw(gs::vecs(gs::integers::<u8>()).min_size(32).max_size(32));
        Some(Ticket { vrf_proof })
    } else {
        None
    };

    // ElectionProof — nullable pointer field
    let has_election: bool = tc.draw(gs::booleans());
    let election_proof = if has_election {
        let win_count: i64 = tc.draw(gs::sampled_from(vec![
            0i64, 1, -1, 5, 100, i64::MAX, i64::MIN + 1,
        ]));
        let vrf_proof: Vec<u8> =
            tc.draw(gs::vecs(gs::integers::<u8>()).min_size(32).max_size(32));
        Some(ElectionProof {
            win_count,
            vrf_proof,
        })
    } else {
        None
    };

    // Parents: 0-2 CIDs
    let num_parents: usize = tc.draw(gs::integers::<usize>().min_value(0).max_value(2));
    let parents: Vec<Cid> = (0..num_parents).map(|_| gen_random_cid()).collect();

    // Parent weight
    let pw_val: u64 = tc.draw(gs::sampled_from(vec![0u64, 1, 100, 999_999_999, u64::MAX]));
    let parent_weight = TokenAmount::from_atto(pw_val);

    // Height
    let height: i64 = tc.draw(gs::sampled_from(vec![0i64, 1, 10, 100, 1000, i64::MAX]));

    // CID fields
    let parent_state_root = gen_random_cid();
    let parent_message_receipts = gen_random_cid();
    let messages = gen_random_cid();

    // BLSAggregate — nullable pointer field
    let has_bls_agg: bool = tc.draw(gs::booleans());
    let bls_aggregate = if has_bls_agg {
        let bytes: Vec<u8> =
            tc.draw(gs::vecs(gs::integers::<u8>()).min_size(96).max_size(96));
        Some(Signature::new_bls(bytes))
    } else {
        None
    };

    let timestamp: u64 = tc.draw(gs::sampled_from(vec![0u64, 1_700_000_000, u64::MAX]));

    // BlockSig — nullable pointer field
    let has_block_sig: bool = tc.draw(gs::booleans());
    let block_sig = if has_block_sig {
        let bytes: Vec<u8> =
            tc.draw(gs::vecs(gs::integers::<u8>()).min_size(96).max_size(96));
        Some(Signature::new_bls(bytes))
    } else {
        None
    };

    let fork_signaling: u64 = tc.draw(gs::sampled_from(vec![0u64, 1, u64::MAX]));
    let parent_base_fee = TokenAmount::from_atto(100u64);

    BlockHeader {
        miner,
        ticket,
        election_proof,
        beacon_entries: vec![],
        win_post_proof: vec![],
        parents,
        parent_weight,
        height,
        parent_state_root,
        parent_message_receipts,
        messages,
        bls_aggregate,
        timestamp,
        block_sig,
        fork_signaling,
        parent_base_fee,
    }
}

/// Generate 0-5 random CIDs.
#[hegel::composite]
fn gen_cid_list(tc: hegel::TestCase) -> Vec<Cid> {
    let count: usize = tc.draw(gs::integers::<usize>().min_value(0).max_value(5));
    (0..count).map(|_| gen_random_cid()).collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[hegel::test(test_cases = 50)]
    fn test_block_msg_is_cbor_array_3(tc: hegel::TestCase) {
        let block_bytes: Vec<u8> = tc.draw(block_msg());
        assert_eq!(block_bytes[0], 0x83, "BlockMsg must be CBOR array(3)");
    }

    #[hegel::test(test_cases = 50)]
    fn test_block_msg_nonempty(tc: hegel::TestCase) {
        let block_bytes: Vec<u8> = tc.draw(block_msg());
        assert!(
            block_bytes.len() > 30,
            "block too short: {} bytes",
            block_bytes.len()
        );
    }
}
