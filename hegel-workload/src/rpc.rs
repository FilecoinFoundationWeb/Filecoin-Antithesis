use log::{debug, warn};
use reqwest::Client;
use serde::Deserialize;
use serde_json::{json, Value};
use std::collections::HashMap;
use std::time::Duration;

const RPC_TIMEOUT: Duration = Duration::from_secs(10);

/// A fault-tolerant JSON-RPC client for Lotus nodes.
/// All methods return Option — None means the node was unreachable or returned
/// an error, which is expected under Antithesis fault injection.
pub struct LotusRpc {
    client: Client,
    url: String,
    token: String,
}

#[derive(Debug, Deserialize)]
struct JsonRpcResponse<T> {
    result: Option<T>,
    error: Option<JsonRpcError>,
}

#[derive(Debug, Deserialize)]
struct JsonRpcError {
    code: i64,
    message: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ChainHead {
    #[serde(rename = "Height")]
    pub height: i64,
}

/// A pending signed message from MpoolPending.
#[derive(Debug, Clone, Deserialize)]
pub struct PendingMessage {
    #[serde(rename = "Message")]
    pub message: PendingInner,
}

#[derive(Debug, Clone, Deserialize)]
pub struct PendingInner {
    #[serde(rename = "From")]
    pub from: String,
    #[serde(rename = "Nonce")]
    pub nonce: u64,
}

impl LotusRpc {
    pub fn new(host: &str, port: u16, token: &str) -> Self {
        let client = Client::builder()
            .timeout(RPC_TIMEOUT)
            .build()
            .expect("failed to build reqwest client");
        Self {
            client,
            url: format!("http://{}:{}/rpc/v1", host, port),
            token: token.to_string(),
        }
    }

    async fn call<T: for<'de> Deserialize<'de>>(&self, method: &str, params: Value) -> Option<T> {
        let body = json!({
            "jsonrpc": "2.0",
            "method": format!("Filecoin.{}", method),
            "params": params,
            "id": 1,
        });

        let resp = self
            .client
            .post(&self.url)
            .header("Authorization", format!("Bearer {}", self.token))
            .header("Content-Type", "application/json")
            .json(&body)
            .send()
            .await;

        let resp = match resp {
            Ok(r) => r,
            Err(e) => {
                debug!("RPC {} failed (network): {}", method, e);
                return None;
            }
        };

        let parsed: JsonRpcResponse<T> = match resp.json().await {
            Ok(p) => p,
            Err(e) => {
                debug!("RPC {} failed (parse): {}", method, e);
                return None;
            }
        };

        if let Some(err) = parsed.error {
            debug!("RPC {} returned error {}: {}", method, err.code, err.message);
            return None;
        }

        parsed.result
    }

    pub async fn chain_head(&self) -> Option<ChainHead> {
        self.call("ChainHead", json!([])).await
    }

    pub async fn mpool_pending(&self) -> Option<Vec<PendingMessage>> {
        // MpoolPending takes a TipSetKey argument; empty array means "current head"
        self.call("MpoolPending", json!([[]])).await
    }

    /// Push a raw signed message (CBOR bytes) to the mempool via RPC.
    /// Returns true if the node accepted the push (regardless of validation outcome).
    pub async fn mpool_push_raw(&self, signed_msg_json: &Value) -> bool {
        let result: Option<Value> = self.call("MpoolPush", json!([signed_msg_json])).await;
        result.is_some()
    }

    /// Call MpoolSelect to exercise the allPending() code path.
    /// The quality parameter controls how aggressively to select messages.
    pub async fn mpool_select(&self, quality: f64) -> Option<Vec<Value>> {
        // MpoolSelect takes (TipSetKey, quality)
        self.call("MpoolSelect", json!([[], quality])).await
    }

    /// Get the next expected nonce for an address from the mempool.
    /// This accounts for pending messages, not just on-chain state.
    pub async fn mpool_get_nonce(&self, address: &str) -> Option<u64> {
        self.call("MpoolGetNonce", serde_json::json!([address]))
            .await
    }
}

/// Discover RPC endpoints for nodes, reading JWT tokens from devgen.
/// Returns a Vec of (node_name, LotusRpc) for all reachable nodes.
pub fn discover_rpc_clients(
    node_names: &[String],
    devgen_dir: &str,
    rpc_port: u16,
) -> Vec<(String, LotusRpc)> {
    let mut clients = Vec::new();
    for name in node_names {
        let token_path = format!("{}/{}/{}-jwt", devgen_dir, name, name);
        let token = match std::fs::read_to_string(&token_path) {
            Ok(t) => t.trim().to_string(),
            Err(e) => {
                warn!("no JWT for {} at {}: {}, trying without auth", name, token_path, e);
                String::new()
            }
        };
        clients.push((name.clone(), LotusRpc::new(name, rpc_port, &token)));
    }
    clients
}

/// Check MpoolPending for duplicate nonces per sender.
/// Returns (is_consistent, details) where details contains any duplicates found.
pub fn check_mpool_consistency(pending: &[PendingMessage]) -> (bool, HashMap<String, Vec<u64>>) {
    let mut nonces_by_sender: HashMap<String, Vec<u64>> = HashMap::new();
    for msg in pending {
        nonces_by_sender
            .entry(msg.message.from.clone())
            .or_default()
            .push(msg.message.nonce);
    }

    let mut duplicates: HashMap<String, Vec<u64>> = HashMap::new();
    let mut consistent = true;

    for (sender, nonces) in &nonces_by_sender {
        let mut seen = std::collections::HashSet::new();
        for &n in nonces {
            if !seen.insert(n) {
                consistent = false;
                duplicates.entry(sender.clone()).or_default().push(n);
            }
        }
    }

    (consistent, duplicates)
}
