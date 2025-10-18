[client]
encrypt_keystore = false
data_dir = "${FOREST_DATA_DIR}"

[network]
kademlia = false
target_peer_count = ${FOREST_TARGET_PEER_COUNT}

# Note that this has to come last. The actual TOML file will have
# the chain name appended.
[chain]
type = "devnet"
