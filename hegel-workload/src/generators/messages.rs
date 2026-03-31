use hegel::generators as gs;

use crate::cbor::*;
use crate::generators::address::filecoin_address;

/// Generate a complete SignedMessage as CBOR: array(2) [Message, Signature].
#[hegel::composite]
pub fn signed_message(tc: hegel::TestCase) -> Vec<u8> {
    let msg = tc.draw(filecoin_message());
    let sig = tc.draw(signature());
    cbor_array(&[&msg, &sig])
}

/// Generate a Filecoin Message as CBOR array(10):
/// [Version, To, From, Nonce, Value, GasLimit, GasFeeCap, GasPremium, Method, Params]
#[hegel::composite]
fn filecoin_message(tc: hegel::TestCase) -> Vec<u8> {
    let version = cbor_uint64(0);

    let to_addr: Vec<u8> = tc.draw(filecoin_address());
    let to = cbor_bytes(&to_addr);

    let from_addr: Vec<u8> = tc.draw(filecoin_address());
    let from = cbor_bytes(&from_addr);

    let nonce_val: u64 =
        tc.draw(gs::sampled_from(vec![0u64, 1, u64::MAX - 1, u64::MAX, 42, 1000, 1_000_000]));
    let nonce = cbor_uint64(nonce_val);

    let value_raw: Vec<u8> = tc.draw(big_int_value());
    let value = cbor_bytes(&value_raw);

    let gas_limit_val: i64 = tc.draw(gs::sampled_from(vec![
        0i64,
        1,
        -1,
        10_000_000,
        i64::MAX,
        i64::MIN + 1,
        100_000,
        -100_000,
    ]));
    let gas_limit = cbor_int64(gas_limit_val);

    let gas_fee_cap_raw: Vec<u8> = tc.draw(big_int_value());
    let gas_fee_cap = cbor_bytes(&gas_fee_cap_raw);

    let gas_premium_raw: Vec<u8> = tc.draw(big_int_value());
    let gas_premium = cbor_bytes(&gas_premium_raw);

    let method_val: u64 = tc.draw(gs::sampled_from(vec![
        0u64, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22,
        23, 24, 25, 26, 27, 28, 29, 30, u64::MAX, 999999,
    ]));
    let method = cbor_uint64(method_val);

    let params = tc.draw(message_params());

    cbor_array(&[
        &version,
        &to,
        &from,
        &nonce,
        &value,
        &gas_limit,
        &gas_fee_cap,
        &gas_premium,
        &method,
        &params,
    ])
}

/// Generate edge-case BigInt values for Value/GasFeeCap/GasPremium fields.
#[hegel::composite]
fn big_int_value(tc: hegel::TestCase) -> Vec<u8> {
    let variant: u8 = tc.draw(gs::integers::<u8>().min_value(0).max_value(4));
    match variant {
        0 => big_int_bytes(0),                  // zero
        1 => big_int_bytes(1),                  // minimal positive
        2 => big_int_bytes(u64::MAX),           // max uint64
        3 => vec![0x00],                        // sign-only (positive sign, zero magnitude)
        _ => {
            // large random value
            let len: usize = tc.draw(gs::integers::<usize>().min_value(1).max_value(16));
            let mut bytes = vec![0x00u8]; // positive sign
            let payload: Vec<u8> =
                tc.draw(gs::vecs(gs::integers::<u8>()).min_size(len).max_size(len));
            bytes.extend(payload);
            bytes
        }
    }
}

/// Generate a Signature as CBOR bytes: type_byte followed by signature data.
#[hegel::composite]
fn signature(tc: hegel::TestCase) -> Vec<u8> {
    let sig_type: u8 = tc.draw(gs::sampled_from(vec![1u8, 2, 0, 3, 0xff]));
    let sig_len: usize = tc.draw(gs::sampled_from(vec![0usize, 1, 64, 65, 96, 48, 128]));

    let mut sig_data = vec![sig_type];
    let payload: Vec<u8> =
        tc.draw(gs::vecs(gs::integers::<u8>()).min_size(sig_len).max_size(sig_len));
    sig_data.extend(payload);
    cbor_bytes(&sig_data)
}

/// Generate message params: empty, valid CBOR empty array, or random bytes.
#[hegel::composite]
fn message_params(tc: hegel::TestCase) -> Vec<u8> {
    let variant: u8 = tc.draw(gs::integers::<u8>().min_value(0).max_value(2));
    match variant {
        0 => cbor_bytes(&[]),            // empty params
        1 => cbor_bytes(&cbor_array(&[])), // valid CBOR empty array as params
        _ => {
            // random bytes
            let len: usize = tc.draw(gs::integers::<usize>().min_value(1).max_value(64));
            let payload: Vec<u8> =
                tc.draw(gs::vecs(gs::integers::<u8>()).min_size(len).max_size(len));
            cbor_bytes(&payload)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[hegel::test(test_cases = 50)]
    fn test_signed_message_is_valid_cbor_structure(tc: hegel::TestCase) {
        let msg_bytes: Vec<u8> = tc.draw(signed_message());
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
