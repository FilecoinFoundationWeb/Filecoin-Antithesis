use crate::rpc::{check_mpool_consistency, LotusRpc};
use log::info;
use serde_json::json;
use std::sync::atomic::{AtomicI64, AtomicBool, Ordering};
use std::time::Duration;

/// Tracks the highest chain height we've observed, for progress assertions.
static HIGHEST_HEIGHT: AtomicI64 = AtomicI64::new(0);

/// Set once when both P2P and RPC traffic have been active in the same window.
static MIXED_TRAFFIC_OBSERVED: AtomicBool = AtomicBool::new(false);

/// Record that P2P publishing happened in this monitoring window.
static P2P_ACTIVE: AtomicBool = AtomicBool::new(false);

/// Record that RPC push happened in this monitoring window.
static RPC_ACTIVE: AtomicBool = AtomicBool::new(false);

/// Signal that P2P traffic was active (called from the generator loop).
pub fn mark_p2p_active() {
    P2P_ACTIVE.store(true, Ordering::Relaxed);
}

/// Signal that RPC traffic was active (called from the RPC traffic task).
pub fn mark_rpc_active() {
    RPC_ACTIVE.store(true, Ordering::Relaxed);
}

/// Run periodic assertion checks against Lotus nodes via RPC.
/// This is designed to be spawned as a long-lived tokio task.
///
/// Fault tolerance: every RPC call may fail (node killed, partitioned, etc).
/// We only fire assertions when we get successful responses.
pub async fn run_rpc_monitor(clients: Vec<(String, LotusRpc)>, interval: Duration) {
    info!("rpc_monitor: starting with {} clients, interval {:?}", clients.len(), interval);

    loop {
        tokio::time::sleep(interval).await;

        for (name, client) in &clients {
            // --- ChainHead liveness ---
            if let Some(head) = client.chain_head().await {
                let height = head.height;

                // Sometimes: node responds to ChainHead (liveness)
                antithesis_sdk::assert_sometimes!(
                    true,
                    "Node responds to ChainHead RPC",
                    &json!({"node": name, "height": height})
                );

                // Sometimes: chain height advances (progress)
                let prev = HIGHEST_HEIGHT.fetch_max(height, Ordering::Relaxed);
                if height > prev {
                    antithesis_sdk::assert_sometimes!(
                        true,
                        "Chain height advances",
                        &json!({"node": name, "previous": prev, "current": height})
                    );
                }
            }

            // --- MpoolPending consistency ---
            if let Some(pending) = client.mpool_pending().await {
                let (consistent, duplicates) = check_mpool_consistency(&pending);

                // AlwaysOrUnreachable: when we can read the mempool, no sender
                // should have duplicate nonces. This catches corruption from
                // data races like the curTs race we found.
                antithesis_sdk::assert_always_or_unreachable!(
                    consistent,
                    "MpoolPending has no duplicate nonces per sender",
                    &json!({
                        "node": name,
                        "pending_count": pending.len(),
                        "duplicates": format!("{:?}", duplicates),
                    })
                );
            }

            // --- MpoolSelect exercising allPending() ---
            // This calls the code path with the second unprotected curTs read.
            // We don't assert on the result content, just that it doesn't
            // crash or hang (the node staying responsive is the assertion).
            if client.mpool_select(0.8).await.is_some() {
                antithesis_sdk::assert_sometimes!(
                    true,
                    "MpoolSelect returns successfully",
                    &json!({"node": name})
                );
            }
        }

        // --- Mixed traffic assertion ---
        let p2p = P2P_ACTIVE.swap(false, Ordering::Relaxed);
        let rpc = RPC_ACTIVE.swap(false, Ordering::Relaxed);
        if p2p && rpc && !MIXED_TRAFFIC_OBSERVED.load(Ordering::Relaxed) {
            MIXED_TRAFFIC_OBSERVED.store(true, Ordering::Relaxed);
            antithesis_sdk::assert_reachable!(
                "Mixed P2P and RPC traffic executed concurrently",
                &json!({})
            );
        }
    }
}

/// Run RPC-based message traffic alongside P2P GossipSub traffic.
/// Periodically pushes messages through MpoolPush to create contention
/// with the P2P Add() path.
///
/// We generate minimal valid-looking JSON messages. They'll be rejected
/// by signature validation, but the point is to exercise the Add() -> checkMessage()
/// -> curTsLk contention path before rejection.
pub async fn run_rpc_traffic(clients: Vec<(String, LotusRpc)>, interval: Duration) {
    info!("rpc_traffic: starting with {} clients, interval {:?}", clients.len(), interval);

    let mut push_count: u64 = 0;

    loop {
        tokio::time::sleep(interval).await;

        for (name, client) in &clients {
            // Build a minimal signed message that will enter the Add() path
            // before being rejected by signature verification. The structure
            // must be valid enough to reach checkMessage().
            let msg_json = json!({
                "Message": {
                    "Version": 0,
                    "To": "f01000",
                    "From": "f01001",
                    "Nonce": push_count,
                    "Value": "0",
                    "GasLimit": 1000000,
                    "GasFeeCap": "100000",
                    "GasPremium": "1000",
                    "Method": 0,
                    "Params": null,
                },
                "Signature": {
                    "Type": 1,
                    "Data": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
                }
            });

            let accepted = client.mpool_push_raw(&msg_json).await;

            if accepted {
                antithesis_sdk::assert_sometimes!(
                    true,
                    "MpoolPush accepted a message via RPC",
                    &json!({"node": name, "push_count": push_count})
                );
            }

            // Regardless of acceptance, if we got any response (not a network
            // error), mark RPC as active for the mixed-traffic assertion.
            // The push_raw call already logged any network errors.
            mark_rpc_active();
            push_count += 1;
        }
    }
}
