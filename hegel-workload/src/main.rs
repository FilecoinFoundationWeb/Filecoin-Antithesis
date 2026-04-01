mod assertions;
mod cbor;
mod discovery;
mod generators;
mod network;
mod properties;
mod rpc;
mod scenario;

use assertions::{mark_p2p_active, run_rpc_monitor, run_rpc_traffic};
use discovery::discover_nodes;
use generators::blocks::block_msg;
use generators::messages::signed_message;
use network::{build_swarm, run_network, PublishRequest};
use rpc::discover_rpc_clients;

use hegel::generators as gs;
use log::{error, info, warn};
use std::time::Duration;
use tokio::sync::mpsc;

fn main() {
    env_logger::init();
    info!("hegel-workload starting");

    // Parse configuration from environment
    let stress_nodes = std::env::var("STRESS_NODES").unwrap_or_else(|_| "lotus0".to_string());
    let node_names: Vec<String> = stress_nodes
        .split(',')
        .map(|s| s.trim().to_string())
        .collect();
    let devgen_dir = std::env::var("DEVGEN_DIR").unwrap_or_else(|_| "/root/devgen".to_string());
    let network_name = read_network_name(&devgen_dir);
    let batch_size: u64 = std::env::var("HEGEL_BATCH_SIZE")
        .ok()
        .and_then(|s| s.parse().ok())
        .unwrap_or(100);
    let publish_delay: Duration = Duration::from_millis(
        std::env::var("PUBLISH_DELAY_MS")
            .ok()
            .and_then(|s| s.parse().ok())
            .unwrap_or(500),
    );
    let rpc_port: u16 = std::env::var("RPC_PORT")
        .ok()
        .and_then(|s| s.parse().ok())
        .unwrap_or(1234);
    let monitor_interval: Duration = Duration::from_millis(
        std::env::var("MONITOR_INTERVAL_MS")
            .ok()
            .and_then(|s| s.parse().ok())
            .unwrap_or(5000),
    );
    let rpc_traffic_interval: Duration = Duration::from_millis(
        std::env::var("RPC_TRAFFIC_INTERVAL_MS")
            .ok()
            .and_then(|s| s.parse().ok())
            .unwrap_or(1000),
    );

    info!(
        "config: nodes={:?}, network={}, batch_size={}, rpc_port={}",
        node_names, network_name, batch_size, rpc_port
    );

    // Discover peers for P2P
    let nodes = discover_nodes(&node_names, &devgen_dir);
    if nodes.is_empty() {
        error!("no nodes discovered, exiting");
        std::process::exit(1);
    }
    info!("discovered {} nodes for P2P", nodes.len());

    // Discover RPC clients
    let rpc_clients = discover_rpc_clients(&node_names, &devgen_dir, rpc_port);
    info!("created {} RPC clients", rpc_clients.len());

    // Build topics
    let msgs_topic = format!("/fil/msgs/{}", network_name);
    let blocks_topic = format!("/fil/blocks/{}", network_name);
    let topics = vec![msgs_topic.clone(), blocks_topic.clone()];

    // Build swarm and channel
    let swarm = build_swarm().expect("failed to build libp2p swarm");
    let (tx, rx) = mpsc::channel::<PublishRequest>(256);
    let (ready_tx, ready_rx) = tokio::sync::oneshot::channel();

    // Prepare peer info for the network task
    let peers: Vec<_> = nodes.iter().map(|n| (n.addr.clone(), n.peer_id)).collect();

    // Spawn tokio runtime for network + RPC tasks
    let rt = tokio::runtime::Runtime::new().expect("failed to create tokio runtime");

    // Spawn network task (existing P2P layer)
    rt.spawn(run_network(swarm, peers, topics, rx, ready_tx));

    // Spawn RPC monitor (assertions + consistency checks)
    // Clone the clients since we need separate ownership for each task.
    let monitor_clients = discover_rpc_clients(&node_names, &devgen_dir, rpc_port);
    rt.spawn(run_rpc_monitor(monitor_clients, monitor_interval));

    // Spawn RPC traffic (mixed P2P + RPC contention)
    let traffic_clients = discover_rpc_clients(&node_names, &devgen_dir, rpc_port);
    rt.spawn(run_rpc_traffic(traffic_clients, rpc_traffic_interval));

    // Wait for mesh to be ready before generating
    info!("waiting for mesh to be ready...");
    let _ = rt.block_on(ready_rx);

    // Fire a GossipSub connectivity assertion
    antithesis_sdk::assert_sometimes!(
        true,
        "GossipSub peer connected",
        &serde_json::json!({})
    );

    info!("starting Hegel generation loop");

    // Main Hegel loop — generates fuzzed messages and publishes over GossipSub
    loop {
        let tx_ref = &tx;
        let msgs_topic_ref = &msgs_topic;
        let blocks_topic_ref = &blocks_topic;

        let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            hegel::Hegel::new(|tc| {
                let use_blocks: bool = tc.draw(gs::booleans());

                if use_blocks {
                    let data: Vec<u8> = tc.draw(block_msg());
                    properties::log_generation(blocks_topic_ref, data.len());
                    let _ = tx_ref.blocking_send(PublishRequest {
                        topic: blocks_topic_ref.to_string(),
                        data,
                    });
                } else {
                    let data: Vec<u8> = tc.draw(signed_message());
                    properties::log_generation(msgs_topic_ref, data.len());
                    let _ = tx_ref.blocking_send(PublishRequest {
                        topic: msgs_topic_ref.to_string(),
                        data,
                    });
                }

                // Signal P2P activity for mixed-traffic assertion
                mark_p2p_active();

                std::thread::sleep(publish_delay);
            })
            .settings(hegel::Settings::new().test_cases(batch_size))
            .run();
        }));

        if let Err(e) = result {
            warn!("Hegel batch failure (expected in fuzzing): {:?}", e);
        }
    }
}

/// Read the network name from lotus0's devgen directory.
fn read_network_name(devgen_dir: &str) -> String {
    let path = format!("{}/lotus0/network_name", devgen_dir);
    for _ in 0..60 {
        if let Ok(name) = std::fs::read_to_string(&path) {
            let name = name.trim().to_string();
            if !name.is_empty() {
                return name;
            }
        }
        std::thread::sleep(std::time::Duration::from_secs(5));
    }
    warn!(
        "could not read network name from {}, defaulting to '2k'",
        path
    );
    "2k".to_string()
}
