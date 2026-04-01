use crate::scenario::types::Wallet;
use cid::Cid;
use fvm_ipld_encoding::to_vec as cbor_serialize;
use fvm_shared::address::Network;
use fvm_shared::crypto::signature::Signature;
use fvm_shared::message::Message;
use k256::ecdsa::{SigningKey, signature::hazmat::PrehashSigner};
use log::{info, warn};
use multihash_codetable::{Code, MultihashDigest};
use serde::Deserialize;

#[derive(Deserialize)]
struct KeystoreEntry {
    #[serde(rename = "Address")]
    address: String,
    #[serde(rename = "PrivateKey")]
    private_key: String,
}

/// Parse a JSON keystore string into a list of Wallet structs.
/// The JSON format is an array of objects with "Address" and "PrivateKey" fields,
/// where PrivateKey is a hex-encoded 32-byte secp256k1 private key.
pub fn parse_keystore(json_str: &str) -> Result<Vec<Wallet>, String> {
    let entries: Vec<KeystoreEntry> =
        serde_json::from_str(json_str).map_err(|e| format!("JSON parse error: {}", e))?;

    let mut wallets = Vec::with_capacity(entries.len());
    for entry in entries {
        // Try mainnet first (f-prefix), fall back to testnet (t-prefix)
        let address = Network::Mainnet
            .parse_address(&entry.address)
            .or_else(|_| Network::Testnet.parse_address(&entry.address))
            .map_err(|e| format!("invalid address '{}': {}", entry.address, e))?;

        let private_key = hex::decode(&entry.private_key)
            .map_err(|e| format!("invalid hex private key for {}: {}", entry.address, e))?;

        if private_key.len() != 32 {
            return Err(format!(
                "private key for {} is {} bytes, expected 32",
                entry.address,
                private_key.len()
            ));
        }

        wallets.push(Wallet {
            address,
            private_key,
        });
    }

    Ok(wallets)
}

/// Load wallets from the stress keystore JSON file at the given path.
/// Retries every 5 seconds for up to 5 minutes if the file does not yet exist.
/// Returns an empty vec on timeout.
pub fn load_keystore(path: &str) -> Vec<Wallet> {
    let max_attempts = 60; // 5 minutes / 5 seconds
    for attempt in 1..=max_attempts {
        match std::fs::read_to_string(path) {
            Ok(contents) => match parse_keystore(&contents) {
                Ok(wallets) => {
                    info!(
                        "loaded {} wallets from keystore '{}'",
                        wallets.len(),
                        path
                    );
                    return wallets;
                }
                Err(e) => {
                    warn!("failed to parse keystore '{}': {}", path, e);
                    return Vec::new();
                }
            },
            Err(_) => {
                if attempt == 1 {
                    info!(
                        "keystore '{}' not yet available, waiting (attempt {}/{})",
                        path, attempt, max_attempts
                    );
                } else {
                    info!(
                        "still waiting for keystore '{}' (attempt {}/{})",
                        path, attempt, max_attempts
                    );
                }
                std::thread::sleep(std::time::Duration::from_secs(5));
            }
        }
    }

    warn!(
        "timed out waiting for keystore '{}' after {} seconds",
        path,
        max_attempts * 5
    );
    Vec::new()
}

pub fn sign_message(msg: &Message, private_key: &[u8]) -> Result<Signature, String> {
    // Step 1: CBOR-serialize the message
    let cbor_bytes = cbor_serialize(msg).map_err(|e| format!("cbor serialize: {}", e))?;

    // Step 2: Compute CID (CIDv1, dag-cbor codec 0x71, SHA2-256)
    let mh = Code::Sha2_256.digest(&cbor_bytes);
    let cid = Cid::new_v1(0x71, mh);
    let cid_bytes = cid.to_bytes();

    // Step 3: Blake2b-256 hash the CID bytes (Filecoin signing convention)
    let hash = blake2b_simd::Params::new()
        .hash_length(32)
        .hash(&cid_bytes);

    // Step 4: ECDSA sign with recovery
    let signing_key =
        SigningKey::from_bytes(private_key.into()).map_err(|e| format!("bad key: {}", e))?;
    let (sig, recovery_id) = signing_key
        .sign_prehash(hash.as_bytes())
        .map_err(|e| format!("sign failed: {}", e))?;

    // Encode as 65 bytes: r (32) || s (32) || v (1)
    let mut sig_bytes = Vec::with_capacity(65);
    sig_bytes.extend_from_slice(&sig.to_bytes());
    sig_bytes.push(recovery_id.to_byte());

    Ok(Signature::new_secp256k1(sig_bytes))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_keystore_json() {
        // Use a valid 32-byte secp256k1 private key (hex = 64 chars).
        // Use testnet address format (t-prefix) matching the production keystore.
        let key_hex = "0101010101010101010101010101010101010101010101010101010101010101";
        let json = format!(
            r#"[{{"Address": "t1d2xrzcslx7xlbbylc5c3d5lvandqw4iwl6epxba", "PrivateKey": "{}"}}]"#,
            key_hex
        );

        let wallets = parse_keystore(&json).expect("should parse successfully");
        assert_eq!(wallets.len(), 1);
        assert_eq!(wallets[0].private_key.len(), 32);
        assert_eq!(wallets[0].private_key, vec![0x01u8; 32]);
    }

    #[test]
    fn test_parse_keystore_empty() {
        let wallets = parse_keystore("[]").expect("should parse empty array");
        assert!(wallets.is_empty());
    }

    #[test]
    fn test_parse_keystore_bad_hex() {
        // Use testnet ID address (t01000) — parse_address(Testnet) handles it.
        let json = r#"[{"Address": "t01000", "PrivateKey": "not_valid_hex!"}]"#;
        let result = parse_keystore(json);
        assert!(result.is_err(), "expected error on invalid hex");
        let err = result.unwrap_err();
        assert!(
            err.contains("invalid hex"),
            "error message should mention 'invalid hex', got: {}",
            err
        );
    }

    #[test]
    fn test_parse_keystore_wrong_key_length() {
        // Only 16 bytes (32 hex chars) instead of 32.
        let short_key = "01020304050607080910111213141516";
        let json = format!(
            r#"[{{"Address": "t01000", "PrivateKey": "{}"}}]"#,
            short_key
        );
        let result = parse_keystore(&json);
        assert!(result.is_err(), "expected error on wrong key length");
        let err = result.unwrap_err();
        assert!(
            err.contains("16 bytes") && err.contains("expected 32"),
            "error message should mention byte length, got: {}",
            err
        );
    }

    #[test]
    fn test_sign_message_produces_65_byte_signature() {
        use fvm_ipld_encoding::RawBytes;
        use fvm_shared::address::Address;
        use fvm_shared::econ::TokenAmount;
        use fvm_shared::message::Message;

        let private_key = vec![1u8; 32];
        let msg = Message {
            version: 0,
            to: Address::new_id(1000),
            from: Address::new_id(1001),
            sequence: 0,
            value: TokenAmount::from_atto(1000u64),
            method_num: 0,
            params: RawBytes::new(vec![]),
            gas_limit: 1_000_000,
            gas_fee_cap: TokenAmount::from_atto(100_000u64),
            gas_premium: TokenAmount::from_atto(1_000u64),
        };

        let sig = sign_message(&msg, &private_key).unwrap();
        assert_eq!(sig.bytes().len(), 65, "secp256k1 recoverable sig must be 65 bytes");
    }

    #[test]
    fn test_sign_message_deterministic() {
        use fvm_ipld_encoding::RawBytes;
        use fvm_shared::address::Address;
        use fvm_shared::econ::TokenAmount;
        use fvm_shared::message::Message;

        let private_key = vec![42u8; 32];
        let msg = Message {
            version: 0,
            to: Address::new_id(1000),
            from: Address::new_id(1001),
            sequence: 5,
            value: TokenAmount::from_atto(0u64),
            method_num: 0,
            params: RawBytes::new(vec![]),
            gas_limit: 1_000_000,
            gas_fee_cap: TokenAmount::from_atto(100_000u64),
            gas_premium: TokenAmount::from_atto(1_000u64),
        };

        let sig1 = sign_message(&msg, &private_key).unwrap();
        let sig2 = sign_message(&msg, &private_key).unwrap();
        assert_eq!(sig1.bytes(), sig2.bytes(), "signing must be deterministic");
    }
}
