use fvm_shared::address::Address;
use fvm_shared::crypto::signature::Signature;
use fvm_shared::message::Message;

use crate::network::PublishRequest;
use crate::rpc::LotusRpc;
use tokio::sync::mpsc;

/// A wallet loaded from the stress keystore.
#[derive(Debug, Clone)]
pub struct Wallet {
    pub address: Address,
    pub private_key: Vec<u8>,
}

/// Observed on-chain state for a wallet.
#[derive(Debug, Clone)]
pub struct WalletState {
    pub wallet: Wallet,
    pub nonce: u64,
}

/// A signed message ready for delivery (not yet published).
#[derive(Debug, Clone)]
pub struct SignedMsg {
    pub message: Message,
    pub signature: Signature,
    pub cbor_bytes: Vec<u8>,
    pub sender_key: Vec<u8>,
}

/// A fuzzed block ready for delivery.
#[derive(Debug, Clone)]
pub struct FuzzedBlock {
    pub cbor_bytes: Vec<u8>,
}

/// Observed chain head.
#[derive(Debug, Clone)]
pub struct ChainTip {
    pub height: i64,
}

/// Snapshot of mempool state.
#[derive(Debug, Clone)]
pub struct MempoolSnapshot {
    pub pending: Vec<(String, u64)>,
}

/// A message confirmed as included on-chain.
#[derive(Debug, Clone)]
pub struct IncludedMsg {
    pub original: SignedMsg,
}

/// I/O handles passed to actions that need network or RPC access.
/// Separate from ScenarioContext so context remains pure data.
pub struct ScenarioIO {
    pub p2p_tx: mpsc::Sender<PublishRequest>,
    pub rpc_clients: Vec<(String, LotusRpc)>,
    pub rt_handle: tokio::runtime::Handle,
    pub msgs_topic: String,
    pub blocks_topic: String,
}
