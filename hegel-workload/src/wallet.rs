use crate::scenario::types::Wallet;
use fvm_shared::address::Network;
use log::{info, warn};
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
        let address = Network::Testnet
            .parse_address(&entry.address)
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
}
