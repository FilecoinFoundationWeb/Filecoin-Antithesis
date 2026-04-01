use libp2p::{
    gossipsub::{self, IdentTopic, MessageAuthenticity, MessageId, ValidationMode},
    identify,
    identity,
    noise,
    swarm::{NetworkBehaviour, SwarmEvent},
    tcp, yamux, Multiaddr, PeerId, Swarm,
};
use log::{info, warn};
use sha2::{Sha256, Digest};
use std::time::Duration;
use tokio::sync::mpsc;
use futures::StreamExt;

/// Messages sent from the generator thread to the network task.
pub struct PublishRequest {
    pub topic: String,
    pub data: Vec<u8>,
}

/// Combined behaviour: GossipSub for pubsub + Identify for protocol negotiation.
/// Lotus requires the identify protocol before accepting GossipSub traffic.
#[derive(NetworkBehaviour)]
pub struct Behaviour {
    gossipsub: gossipsub::Behaviour,
    identify: identify::Behaviour,
}

/// Build a libp2p Swarm with GossipSub + Identify configured for Filecoin interop.
pub fn build_swarm() -> Result<Swarm<Behaviour>, Box<dyn std::error::Error>> {
    let local_key = identity::Keypair::generate_ed25519();

    // Content-hash message ID function — critical for Lotus interop.
    let message_id_fn = |message: &gossipsub::Message| -> MessageId {
        let mut hasher = Sha256::new();
        hasher.update(&message.data);
        MessageId::from(hasher.finalize().to_vec())
    };

    let gossipsub_config = gossipsub::ConfigBuilder::default()
        .heartbeat_interval(Duration::from_secs(1))
        .validation_mode(ValidationMode::Permissive)
        .message_id_fn(message_id_fn)
        .build()
        .map_err(|e| format!("gossipsub config error: {}", e))?;

    let gossipsub = gossipsub::Behaviour::new(
        MessageAuthenticity::Signed(local_key.clone()),
        gossipsub_config,
    )
    .map_err(|e| format!("gossipsub behaviour error: {}", e))?;

    let identify = identify::Behaviour::new(identify::Config::new(
        "/fil/hegel/0.1.0".to_string(),
        local_key.public(),
    ));

    let swarm = libp2p::SwarmBuilder::with_existing_identity(local_key)
        .with_tokio()
        .with_tcp(
            tcp::Config::default(),
            noise::Config::new,
            yamux::Config::default,
        )?
        .with_behaviour(|_| Behaviour { gossipsub, identify })?
        .with_swarm_config(|c| c.with_idle_connection_timeout(Duration::from_secs(60)))
        .build();

    Ok(swarm)
}

/// Run the network event loop. Connects to peers, subscribes to topics, and
/// publishes messages received on the `rx` channel.
///
/// Sends a signal on `ready_tx` once at least one peer has joined the mesh
/// for any subscribed topic, so the generator knows it can start publishing.
pub async fn run_network(
    mut swarm: Swarm<Behaviour>,
    peers: Vec<(Multiaddr, PeerId)>,
    topics: Vec<String>,
    mut rx: mpsc::Receiver<PublishRequest>,
    ready_tx: tokio::sync::oneshot::Sender<()>,
) {
    // Listen on random port
    swarm
        .listen_on("/ip4/0.0.0.0/tcp/0".parse().unwrap())
        .expect("failed to listen");

    // Register peers as explicit GossipSub peers and dial them.
    // Explicit peers always receive published messages regardless of mesh state,
    // which avoids InsufficientPeers errors when Go/Rust GossipSub protocol
    // negotiation doesn't fully establish a mesh.
    for (addr, peer_id) in &peers {
        swarm.behaviour_mut().gossipsub.add_explicit_peer(peer_id);
        let dial_addr = addr.clone().with(libp2p::multiaddr::Protocol::P2p(*peer_id));
        match swarm.dial(dial_addr.clone()) {
            Ok(_) => info!("dialing {} (explicit peer)", dial_addr),
            Err(e) => warn!("failed to dial {}: {}", dial_addr, e),
        }
    }

    // Subscribe to topics
    for t in &topics {
        let topic = IdentTopic::new(t);
        if let Err(e) = swarm.behaviour_mut().gossipsub.subscribe(&topic) {
            warn!("failed to subscribe to {}: {}", t, e);
        } else {
            info!("subscribed to {}", t);
        }
    }

    // Wait for at least one connection to be established (up to 60s).
    // With explicit peers, we don't need mesh formation — just a live connection.
    info!("waiting for peer connections...");
    let deadline = tokio::time::Instant::now() + Duration::from_secs(60);
    let mut connected = false;
    let mut ready_tx = Some(ready_tx);
    while tokio::time::Instant::now() < deadline && !connected {
        tokio::select! {
            _ = tokio::time::sleep_until(deadline) => { break; }
            event = swarm.select_next_some() => {
                if let SwarmEvent::ConnectionEstablished { peer_id, .. } = event {
                    info!("connected to {} (explicit peer)", peer_id);
                    connected = true;
                }
            }
        }
    }
    if connected {
        // Brief pause for identify exchange + gossipsub subscription propagation
        tokio::time::sleep(Duration::from_secs(3)).await;
        info!("peer connected, signalling ready");
    } else {
        warn!("no peer connections after 60s, proceeding anyway");
    }
    if let Some(tx) = ready_tx.take() {
        let _ = tx.send(());
    }
    info!("entering event loop");

    // Main event loop
    loop {
        tokio::select! {
            event = swarm.select_next_some() => {
                match event {
                    SwarmEvent::ConnectionEstablished { peer_id, .. } => {
                        info!("connected to {}", peer_id);
                    }
                    SwarmEvent::ConnectionClosed { peer_id, .. } => {
                        warn!("disconnected from {}", peer_id);
                    }
                    _ => {}
                }
            }
            Some(req) = rx.recv() => {
                let topic = IdentTopic::new(&req.topic);
                match swarm.behaviour_mut().gossipsub.publish(topic, req.data) {
                    Ok(msg_id) => {
                        log::debug!("published to {}: {:?}", req.topic, msg_id);
                    }
                    Err(e) => {
                        warn!("publish to {} failed: {}", req.topic, e);
                    }
                }
            }
        }
    }
}
