use crate::generators::blocks::block_msg;
use crate::generators::messages::signed_message;
use crate::scenario::types::*;
use crate::wallet::sign_message;
use fvm_ipld_encoding::{to_vec as cbor_serialize, RawBytes};
use fvm_shared::address::Address;
use fvm_shared::crypto::signature::Signature;
use fvm_shared::econ::TokenAmount;
use fvm_shared::message::Message;
use hegel::generators as gs;

/// Draw a random wallet from the loaded keystore.
pub fn pick_wallet(tc: hegel::TestCase, wallets: &[Wallet]) -> Wallet {
    let idx: usize = tc.draw(gs::integers::<usize>().min_value(0).max_value(wallets.len() - 1));
    wallets[idx].clone()
}

/// Sign a Message and package it into a SignedMsg.
pub fn build_signed_msg(msg: Message, private_key: &[u8]) -> SignedMsg {
    let signature = sign_message(&msg, private_key).expect("signing should not fail");
    let cbor_bytes =
        cbor_serialize(&(&msg, &signature)).expect("CBOR serialization should not fail");
    SignedMsg {
        message: msg,
        signature,
        cbor_bytes,
        sender_key: private_key.to_vec(),
    }
}

/// Build a validly-signed transfer message.
/// Uses the correct nonce from sender state, method_num=0 (plain transfer).
/// Fuzzes: value amount, gas_limit, gas_premium.
pub fn create_valid_transfer(
    tc: hegel::TestCase,
    sender: &WalletState,
    recipient: &Wallet,
) -> SignedMsg {
    let value_atto: u64 = tc.draw(gs::sampled_from(vec![
        0u64,
        1,
        1_000,
        1_000_000,
        1_000_000_000_000_000_000, // 1 FIL
    ]));
    let gas_limit: u64 = tc.draw(gs::sampled_from(vec![
        1_000_000u64,
        10_000_000,
        50_000_000,
        100_000_000,
    ]));
    let gas_premium_atto: u64 = tc.draw(gs::sampled_from(vec![
        1_000u64,
        10_000,
        100_000,
        1_000_000,
    ]));

    let msg = Message {
        version: 0,
        to: recipient.address,
        from: sender.wallet.address,
        sequence: sender.nonce,
        value: TokenAmount::from_atto(value_atto),
        method_num: 0,
        params: RawBytes::new(vec![]),
        gas_limit,
        gas_fee_cap: TokenAmount::from_atto(gas_premium_atto * 2),
        gas_premium: TokenAmount::from_atto(gas_premium_atto),
    };

    build_signed_msg(msg, &sender.wallet.private_key)
}

/// Same sender+nonce as original, different recipient. Valid signature.
pub fn create_nonce_reuse(
    tc: hegel::TestCase,
    original: &SignedMsg,
    other_wallets: &[Wallet],
) -> SignedMsg {
    let idx: usize =
        tc.draw(gs::integers::<usize>().min_value(0).max_value(other_wallets.len() - 1));
    let new_recipient = &other_wallets[idx];

    let value_atto: u64 = tc.draw(gs::sampled_from(vec![
        0u64,
        1,
        1_000,
        1_000_000,
    ]));

    let msg = Message {
        version: 0,
        to: new_recipient.address,
        from: original.message.from,
        sequence: original.message.sequence,
        value: TokenAmount::from_atto(value_atto),
        method_num: 0,
        params: RawBytes::new(vec![]),
        gas_limit: original.message.gas_limit,
        gas_fee_cap: original.message.gas_fee_cap.clone(),
        gas_premium: original.message.gas_premium.clone(),
    };

    build_signed_msg(msg, &original.sender_key)
}

/// Same sender+nonce, higher gas_premium. Valid signature.
pub fn create_gas_bump(tc: hegel::TestCase, original: &SignedMsg) -> SignedMsg {
    let multiplier: u64 = tc.draw(gs::sampled_from(vec![2u64, 5, 10, 50]));

    // Extract a base premium value. Since TokenAmount is BigInt, we use a pragmatic
    // approach: serialize the original premium and if it's small enough, multiply it.
    // Otherwise use a fixed base.
    let base_premium: u64 = 1_000_000; // fallback base
    let new_premium_atto = base_premium * multiplier;

    // Ensure the new premium is strictly greater than the original by also adding
    // a bump on top of whatever the original was.
    // We construct a new premium that's guaranteed larger.
    let msg = Message {
        version: 0,
        to: original.message.to,
        from: original.message.from,
        sequence: original.message.sequence,
        value: original.message.value.clone(),
        method_num: original.message.method_num,
        params: original.message.params.clone(),
        gas_limit: original.message.gas_limit,
        gas_fee_cap: TokenAmount::from_atto(new_premium_atto * 2),
        gas_premium: &original.message.gas_premium + TokenAmount::from_atto(new_premium_atto),
    };

    build_signed_msg(msg, &original.sender_key)
}

/// Valid transfer but with nonce = current + gap (2..10). Skips ahead.
pub fn create_nonce_gap(
    tc: hegel::TestCase,
    sender: &WalletState,
    recipient: &Wallet,
) -> SignedMsg {
    let gap: u64 = tc.draw(gs::integers::<u64>().min_value(2).max_value(10));

    let value_atto: u64 = tc.draw(gs::sampled_from(vec![
        0u64, 1, 1_000, 1_000_000,
    ]));
    let gas_limit: u64 = tc.draw(gs::sampled_from(vec![
        1_000_000u64, 10_000_000, 50_000_000,
    ]));
    let gas_premium_atto: u64 = tc.draw(gs::sampled_from(vec![
        1_000u64, 10_000, 100_000,
    ]));

    let msg = Message {
        version: 0,
        to: recipient.address,
        from: sender.wallet.address,
        sequence: sender.nonce + gap,
        value: TokenAmount::from_atto(value_atto),
        method_num: 0,
        params: RawBytes::new(vec![]),
        gas_limit,
        gas_fee_cap: TokenAmount::from_atto(gas_premium_atto * 2),
        gas_premium: TokenAmount::from_atto(gas_premium_atto),
    };

    build_signed_msg(msg, &sender.wallet.private_key)
}

/// Valid sender/nonce/signature but fuzzed method_num, gas values, params.
pub fn create_semi_valid_msg(
    tc: hegel::TestCase,
    sender: &WalletState,
    recipient: &Wallet,
) -> SignedMsg {
    let method_num: u64 = tc.draw(gs::sampled_from(vec![
        0u64, 1, 2, 3, 4, 5, 6, 7, 8, 16, 24, 999999, u64::MAX,
    ]));

    let gas_limit: u64 = tc.draw(gs::sampled_from(vec![
        0u64, 1, 10_000_000, u64::MAX,
    ]));

    let gas_premium_atto: u64 = tc.draw(gs::sampled_from(vec![
        0u64, 1, u64::MAX,
    ]));

    let gas_fee_cap_atto: u64 = tc.draw(gs::sampled_from(vec![
        0u64, 1, u64::MAX,
    ]));

    let value_atto: u64 = tc.draw(gs::sampled_from(vec![
        0u64, 1, u64::MAX,
    ]));

    // Fuzz params: empty, CBOR empty array, or random bytes
    let params_variant: u8 = tc.draw(gs::integers::<u8>().min_value(0).max_value(2));
    let params = match params_variant {
        0 => RawBytes::new(vec![]),
        1 => RawBytes::new(vec![0x80]), // CBOR empty array
        _ => {
            let len: usize = tc.draw(gs::integers::<usize>().min_value(1).max_value(64));
            let payload: Vec<u8> =
                tc.draw(gs::vecs(gs::integers::<u8>()).min_size(len).max_size(len));
            RawBytes::new(payload)
        }
    };

    let msg = Message {
        version: 0,
        to: recipient.address,
        from: sender.wallet.address,
        sequence: sender.nonce,
        value: TokenAmount::from_atto(value_atto),
        method_num,
        params,
        gas_limit,
        gas_fee_cap: TokenAmount::from_atto(gas_fee_cap_atto),
        gas_premium: TokenAmount::from_atto(gas_premium_atto),
    };

    build_signed_msg(msg, &sender.wallet.private_key)
}

/// Fully fuzzed message using the existing signed_message() generator.
/// Stores placeholder values in the message field since we can't decompose the CBOR.
/// The cbor_bytes field is what matters for publishing.
pub fn create_fuzzed_msg(tc: hegel::TestCase) -> SignedMsg {
    let cbor_bytes: Vec<u8> = tc.draw(signed_message());

    SignedMsg {
        message: Message {
            version: 0,
            to: Address::new_id(0),
            from: Address::new_id(0),
            sequence: 0,
            value: TokenAmount::from_atto(0u64),
            method_num: 0,
            params: RawBytes::new(vec![]),
            gas_limit: 0,
            gas_fee_cap: TokenAmount::from_atto(0u64),
            gas_premium: TokenAmount::from_atto(0u64),
        },
        signature: Signature::new_secp256k1(vec![0u8; 65]),
        cbor_bytes,
        sender_key: vec![],
    }
}

/// Fully fuzzed block using the existing block_msg() generator.
pub fn create_fuzzed_block(tc: hegel::TestCase) -> FuzzedBlock {
    let cbor_bytes: Vec<u8> = tc.draw(block_msg());
    FuzzedBlock { cbor_bytes }
}

#[cfg(test)]
mod tests {
    use super::*;
    use fvm_shared::address::Address;

    fn test_wallet(id: u64) -> Wallet {
        Wallet {
            address: Address::new_id(id),
            private_key: vec![id as u8; 32],
        }
    }

    fn test_wallet_state(id: u64, nonce: u64) -> WalletState {
        WalletState {
            wallet: test_wallet(id),
            nonce,
        }
    }

    #[hegel::test(test_cases = 20)]
    fn test_create_valid_transfer_has_correct_nonce(tc: hegel::TestCase) {
        let sender_state = test_wallet_state(1, 42);
        let recipient = test_wallet(2);
        let msg = create_valid_transfer(tc, &sender_state, &recipient);
        assert_eq!(msg.message.sequence, 42);
        assert_eq!(msg.message.from, sender_state.wallet.address);
        assert_eq!(msg.message.to, recipient.address);
        assert_eq!(msg.signature.bytes().len(), 65);
    }

    #[hegel::test(test_cases = 20)]
    fn test_create_nonce_reuse_keeps_nonce(tc: hegel::TestCase) {
        let sender_state = test_wallet_state(1, 10);
        let recipient = test_wallet(2);
        let original = create_valid_transfer(tc.clone(), &sender_state, &recipient);
        let other_wallets = vec![test_wallet(3), test_wallet(4)];
        let reused = create_nonce_reuse(tc, &original, &other_wallets);
        assert_eq!(reused.message.sequence, original.message.sequence);
        assert_eq!(reused.message.from, original.message.from);
    }

    #[hegel::test(test_cases = 20)]
    fn test_create_gas_bump_higher_premium(tc: hegel::TestCase) {
        let sender_state = test_wallet_state(1, 5);
        let recipient = test_wallet(2);
        let original = create_valid_transfer(tc.clone(), &sender_state, &recipient);
        let bumped = create_gas_bump(tc, &original);
        assert_eq!(bumped.message.sequence, original.message.sequence);
        assert!(bumped.message.gas_premium > original.message.gas_premium);
    }

    #[hegel::test(test_cases = 20)]
    fn test_create_nonce_gap_skips_ahead(tc: hegel::TestCase) {
        let sender_state = test_wallet_state(1, 10);
        let recipient = test_wallet(2);
        let msg = create_nonce_gap(tc, &sender_state, &recipient);
        assert!(msg.message.sequence > sender_state.nonce);
    }

    #[hegel::test(test_cases = 20)]
    fn test_create_fuzzed_msg_is_valid_cbor(tc: hegel::TestCase) {
        let msg = create_fuzzed_msg(tc);
        assert_eq!(msg.cbor_bytes[0], 0x82, "SignedMessage must be CBOR array(2)");
    }
}
