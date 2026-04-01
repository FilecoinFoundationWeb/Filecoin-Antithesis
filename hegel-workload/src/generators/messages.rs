use fvm_ipld_encoding::{RawBytes, to_vec};
use fvm_shared::address::Address;
use fvm_shared::crypto::signature::Signature;
use fvm_shared::econ::TokenAmount;
use fvm_shared::message::Message;
use hegel::generators as gs;

use crate::generators::address::filecoin_address;

/// Generate a complete SignedMessage as DAG-CBOR bytes.
/// Serialized as a CBOR tuple (Message, Signature) matching Lotus wire format.
#[hegel::composite]
pub fn signed_message(tc: hegel::TestCase) -> Vec<u8> {
    let msg = tc.draw(filecoin_message());
    let sig = tc.draw(fuzz_signature());
    // Lotus encodes SignedMessage as CBOR array [Message, Signature]
    to_vec(&(&msg, &sig)).expect("DAG-CBOR serialization of SignedMessage should not fail")
}

/// Generate a Filecoin Message with structurally valid fields but fuzzed values.
/// Addresses are valid format, values are edge-case, nonces/methods are fuzzed.
#[hegel::composite]
fn filecoin_message(tc: hegel::TestCase) -> Message {
    let to: Address = tc.draw(filecoin_address());
    let from: Address = tc.draw(filecoin_address());

    let sequence: u64 = tc.draw(gs::sampled_from(vec![
        0u64, 1, 42, 1000, 1_000_000, u64::MAX,
    ]));

    let value = tc.draw(fuzz_token_amount());
    let gas_limit: u64 = tc.draw(gs::sampled_from(vec![
        0u64, 1, 10_000_000, 100_000, u64::MAX,
    ]));
    let gas_fee_cap = tc.draw(fuzz_token_amount());
    let gas_premium = tc.draw(fuzz_token_amount());

    let method_num: u64 = tc.draw(gs::sampled_from(vec![
        0u64, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 16, 20, 24, u64::MAX, 999999,
    ]));

    let params = tc.draw(fuzz_params());

    Message {
        version: 0,
        to,
        from,
        sequence,
        value,
        method_num,
        params,
        gas_limit,
        gas_fee_cap,
        gas_premium,
    }
}

/// Generate fuzzed TokenAmount values (BigInt).
#[hegel::composite]
fn fuzz_token_amount(tc: hegel::TestCase) -> TokenAmount {
    let variant: u8 = tc.draw(gs::integers::<u8>().min_value(0).max_value(4));
    match variant {
        0 => TokenAmount::from_atto(0),
        1 => TokenAmount::from_atto(1),
        2 => TokenAmount::from_atto(u64::MAX),
        3 => TokenAmount::from_atto(1_000_000_000_000_000_000u64), // 1 FIL
        _ => {
            let v: u64 = tc.draw(gs::integers::<u64>());
            TokenAmount::from_atto(v)
        }
    }
}

/// Generate a signature with valid type but random (incorrect) bytes.
/// This passes decoding but fails signature verification.
#[hegel::composite]
fn fuzz_signature(tc: hegel::TestCase) -> Signature {
    let use_bls: bool = tc.draw(gs::booleans());
    if use_bls {
        // BLS signatures are 96 bytes
        let bytes: Vec<u8> = tc.draw(gs::vecs(gs::integers::<u8>()).min_size(96).max_size(96));
        Signature::new_bls(bytes)
    } else {
        // Secp256k1 signatures are 65 bytes
        let bytes: Vec<u8> = tc.draw(gs::vecs(gs::integers::<u8>()).min_size(65).max_size(65));
        Signature::new_secp256k1(bytes)
    }
}

/// Generate message params: empty or random CBOR-compatible bytes.
#[hegel::composite]
fn fuzz_params(tc: hegel::TestCase) -> RawBytes {
    let variant: u8 = tc.draw(gs::integers::<u8>().min_value(0).max_value(2));
    match variant {
        0 => RawBytes::new(vec![]),
        1 => {
            // Valid CBOR empty array as params
            RawBytes::new(vec![0x80])
        }
        _ => {
            let len: usize = tc.draw(gs::integers::<usize>().min_value(1).max_value(64));
            let payload: Vec<u8> =
                tc.draw(gs::vecs(gs::integers::<u8>()).min_size(len).max_size(len));
            RawBytes::new(payload)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[hegel::test(test_cases = 50)]
    fn test_signed_message_is_valid_cbor(tc: hegel::TestCase) {
        let msg_bytes: Vec<u8> = tc.draw(signed_message());
        // Should start with CBOR array(2) header
        assert_eq!(msg_bytes[0], 0x82, "SignedMessage must be CBOR array(2)");
    }

    #[hegel::test(test_cases = 50)]
    fn test_signed_message_nonempty(tc: hegel::TestCase) {
        let msg_bytes: Vec<u8> = tc.draw(signed_message());
        assert!(
            msg_bytes.len() > 10,
            "message too short: {} bytes",
            msg_bytes.len()
        );
    }
}
