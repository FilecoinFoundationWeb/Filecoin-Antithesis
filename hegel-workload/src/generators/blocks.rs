use hegel::generators as gs;

use crate::cbor::*;
use crate::generators::address::filecoin_address;

/// Generate a `BlockMsg` as CBOR array(3): [Header, BlsMessages []CID, SecpkMessages []CID].
#[hegel::composite]
pub fn block_msg(tc: hegel::TestCase) -> Vec<u8> {
    let header = tc.draw(block_header());
    let bls_msgs = tc.draw(cid_list());
    let secpk_msgs = tc.draw(cid_list());
    cbor_array(&[&header, &bls_msgs, &secpk_msgs])
}

/// Generate a Filecoin block header as a 16-field CBOR array:
/// [Miner, Ticket, ElectionProof, BeaconEntries, WinPoStProof, Parents,
///  ParentWeight, Height, ParentStateRoot, ParentMessageReceipts, Messages,
///  BLSAggregate, Timestamp, BlockSig, ForkSignaling, ParentBaseFee]
#[hegel::composite]
fn block_header(tc: hegel::TestCase) -> Vec<u8> {
    // Miner address
    let miner_addr: Vec<u8> = tc.draw(filecoin_address());
    let miner = cbor_bytes(&miner_addr);

    // Ticket
    let ticket = tc.draw(ticket_field());

    // ElectionProof
    let election_proof = tc.draw(election_proof_field());

    // BeaconEntries: nil or empty array
    let beacon_variant: u8 = tc.draw(gs::integers::<u8>().min_value(0).max_value(1));
    let beacon_entries = match beacon_variant {
        0 => cbor_nil(),
        _ => cbor_array(&[]),
    };

    // WinPoStProof: always empty array
    let win_post_proof = cbor_array(&[]);

    // Parents
    let parents = tc.draw(parent_cids());

    // ParentWeight
    let pw_val: u64 = tc.draw(gs::sampled_from(vec![0u64, 1, 100, 999_999_999, u64::MAX]));
    let parent_weight = cbor_bytes(&big_int_bytes(pw_val));

    // Height
    let h_val: u64 = tc.draw(gs::sampled_from(vec![
        0u64,
        1,
        10,
        100,
        1000,
        u64::MAX,
        u64::MAX - 1,
    ]));
    let height = cbor_uint64(h_val);

    // ParentStateRoot, ParentMessageReceipts, Messages — random CIDs
    let state_root_cid = random_cid();
    let parent_state_root = cbor_cid(&state_root_cid);

    let msg_receipts_cid = random_cid();
    let parent_msg_receipts = cbor_cid(&msg_receipts_cid);

    let messages_cid = random_cid();
    let messages = cbor_cid(&messages_cid);

    // BLSAggregate: nil or BLS type byte
    let bls_agg_variant: u8 = tc.draw(gs::integers::<u8>().min_value(0).max_value(1));
    let bls_aggregate = match bls_agg_variant {
        0 => cbor_nil(),
        _ => cbor_bytes(&[0x02]),
    };

    // Timestamp: fixed
    let timestamp = cbor_uint64(1_700_000_000);

    // BlockSig: nil or BLS sig with random 8 bytes
    let sig_variant: u8 = tc.draw(gs::integers::<u8>().min_value(0).max_value(1));
    let block_sig = match sig_variant {
        0 => cbor_nil(),
        _ => {
            let sig_payload: Vec<u8> =
                tc.draw(gs::vecs(gs::integers::<u8>()).min_size(8).max_size(8));
            let mut sig_data = vec![0x02u8]; // BLS type
            sig_data.extend(sig_payload);
            cbor_bytes(&sig_data)
        }
    };

    // ForkSignaling: 0
    let fork_signaling = cbor_uint64(0);

    // ParentBaseFee
    let parent_base_fee = cbor_bytes(&big_int_bytes(100));

    cbor_array(&[
        &miner,
        &ticket,
        &election_proof,
        &beacon_entries,
        &win_post_proof,
        &parents,
        &parent_weight,
        &height,
        &parent_state_root,
        &parent_msg_receipts,
        &messages,
        &bls_aggregate,
        &timestamp,
        &block_sig,
        &fork_signaling,
        &parent_base_fee,
    ])
}

/// Ticket field: nil, valid [VRFProof 32 bytes], or empty array.
#[hegel::composite]
fn ticket_field(tc: hegel::TestCase) -> Vec<u8> {
    let variant: u8 = tc.draw(gs::integers::<u8>().min_value(0).max_value(2));
    match variant {
        0 => cbor_nil(),
        1 => {
            let vrf_proof: Vec<u8> =
                tc.draw(gs::vecs(gs::integers::<u8>()).min_size(32).max_size(32));
            let proof_bytes = cbor_bytes(&vrf_proof);
            cbor_array(&[&proof_bytes])
        }
        _ => cbor_array(&[]),
    }
}

/// ElectionProof field: nil or [WinCount i64, VRFProof 32 bytes].
#[hegel::composite]
fn election_proof_field(tc: hegel::TestCase) -> Vec<u8> {
    let variant: u8 = tc.draw(gs::integers::<u8>().min_value(0).max_value(1));
    match variant {
        0 => cbor_nil(),
        _ => {
            let win_count_val: i64 =
                tc.draw(gs::sampled_from(vec![0i64, 1, -1, 5, 100, i64::MAX, i64::MIN + 1]));
            let win_count = cbor_int64(win_count_val);
            let vrf_proof: Vec<u8> =
                tc.draw(gs::vecs(gs::integers::<u8>()).min_size(32).max_size(32));
            let proof_bytes = cbor_bytes(&vrf_proof);
            cbor_array(&[&win_count, &proof_bytes])
        }
    }
}

/// Parent CIDs: nil, empty, single CID, or two CIDs.
#[hegel::composite]
fn parent_cids(tc: hegel::TestCase) -> Vec<u8> {
    let variant: u8 = tc.draw(gs::integers::<u8>().min_value(0).max_value(3));
    match variant {
        0 => cbor_nil(),
        1 => cbor_array(&[]),
        2 => {
            let cid = random_cid();
            let c = cbor_cid(&cid);
            cbor_array(&[&c])
        }
        _ => {
            let cid1 = random_cid();
            let c1 = cbor_cid(&cid1);
            let cid2 = random_cid();
            let c2 = cbor_cid(&cid2);
            cbor_array(&[&c1, &c2])
        }
    }
}

/// CID list: 0-5 random CIDs for BlsMessages/SecpkMessages.
#[hegel::composite]
fn cid_list(tc: hegel::TestCase) -> Vec<u8> {
    let count: usize = tc.draw(gs::integers::<usize>().min_value(0).max_value(5));
    let cids: Vec<Vec<u8>> = (0..count).map(|_| {
        let raw = random_cid();
        cbor_cid(&raw)
    }).collect();
    let refs: Vec<&[u8]> = cids.iter().map(|c| c.as_slice()).collect();
    cbor_array(&refs)
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
        assert!(block_bytes.len() > 30, "block too short: {} bytes", block_bytes.len());
    }
}
