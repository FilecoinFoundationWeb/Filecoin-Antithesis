use fvm_shared::address::Address;
use hegel::generators as gs;

/// Generate a structurally valid Filecoin Address.
/// Uses fvm_shared constructors to guarantee correct encoding.
/// Values are fuzzed (random keys, arbitrary actor IDs) so that
/// semantic validation fails deeper in the pipeline.
#[hegel::composite]
pub fn filecoin_address(tc: hegel::TestCase) -> Address {
    let protocol: u8 = tc.draw(gs::sampled_from(vec![0u8, 1, 2, 3, 4]));
    match protocol {
        0 => tc.draw(id_address()),
        1 => tc.draw(secp256k1_address()),
        2 => tc.draw(actor_address()),
        3 => tc.draw(bls_address()),
        _ => tc.draw(delegated_address()),
    }
}

/// ID address with fuzzed actor IDs (system actors, miners, nonexistent, boundary).
#[hegel::composite]
pub fn id_address(tc: hegel::TestCase) -> Address {
    let actor_id: u64 = tc.draw(gs::sampled_from(vec![
        0u64,           // system actor
        1,              // init actor
        2,              // reward actor
        3,              // cron actor
        4,              // storage power actor
        5,              // storage market actor
        6,              // verified registry actor
        7,              // datacap actor
        10,             // EAM actor
        99,             // burnt funds actor
        1000,           // first miner
        1001,           // second miner
        2000,           // some account
        999_999,        // high actor ID
    ]));
    Address::new_id(actor_id)
}

/// Secp256k1 address with random 65-byte public key (won't match any real actor).
#[hegel::composite]
pub fn secp256k1_address(tc: hegel::TestCase) -> Address {
    // Secp256k1 public keys are 65 bytes (uncompressed)
    let pubkey: Vec<u8> =
        tc.draw(gs::vecs(gs::integers::<u8>()).min_size(65).max_size(65));
    Address::new_secp256k1(&pubkey).expect("65-byte pubkey should produce valid secp256k1 address")
}

/// Actor address with random 32-byte data.
#[hegel::composite]
pub fn actor_address(tc: hegel::TestCase) -> Address {
    let data: Vec<u8> =
        tc.draw(gs::vecs(gs::integers::<u8>()).min_size(32).max_size(32));
    Address::new_actor(&data)
}

/// BLS address with random 48-byte public key.
#[hegel::composite]
pub fn bls_address(tc: hegel::TestCase) -> Address {
    let pubkey: Vec<u8> =
        tc.draw(gs::vecs(gs::integers::<u8>()).min_size(48).max_size(48));
    Address::new_bls(&pubkey).expect("48-byte pubkey should produce valid BLS address")
}

/// Delegated address with fuzzed namespace and sub-address.
#[hegel::composite]
pub fn delegated_address(tc: hegel::TestCase) -> Address {
    let namespace: u64 = tc.draw(gs::sampled_from(vec![
        10u64,  // EAM (Ethereum Address Manager)
        1,      // init actor namespace
        99,     // arbitrary
        1000,   // high namespace
    ]));
    let sub_len: usize = tc.draw(gs::sampled_from(vec![20usize, 1, 32, 54]));
    let subaddr: Vec<u8> =
        tc.draw(gs::vecs(gs::integers::<u8>()).min_size(sub_len).max_size(sub_len));
    Address::new_delegated(namespace, &subaddr)
        .expect("delegated address construction should not fail")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[hegel::test(test_cases = 50)]
    fn test_filecoin_address_roundtrips(tc: hegel::TestCase) {
        let addr: Address = tc.draw(filecoin_address());
        // Verify the address can be serialized to bytes and back
        let bytes = addr.to_bytes();
        let recovered = Address::from_bytes(&bytes).expect("address should roundtrip");
        assert_eq!(addr, recovered);
    }

    #[hegel::test(test_cases = 50)]
    fn test_id_address_valid(tc: hegel::TestCase) {
        let addr: Address = tc.draw(id_address());
        assert_eq!(addr.protocol(), fvm_shared::address::Protocol::ID);
    }

    #[hegel::test(test_cases = 50)]
    fn test_bls_address_valid(tc: hegel::TestCase) {
        let addr: Address = tc.draw(bls_address());
        assert_eq!(addr.protocol(), fvm_shared::address::Protocol::BLS);
    }
}
