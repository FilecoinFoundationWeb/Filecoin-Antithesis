# Template file — placeholders are substituted by start-forest.sh at runtime.
[client]
encrypt_keystore = false
data_dir = "${FOREST_DATA_DIR}"

[network]
# Kademlia disabled for controlled test environment
kademlia = false
target_peer_count = ${FOREST_TARGET_PEER_COUNT}

# Chain section must come last — the network name is appended at runtime.
[chain]
type = "devnet"
