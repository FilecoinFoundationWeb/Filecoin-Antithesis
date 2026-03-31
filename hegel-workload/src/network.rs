use libp2p::{
    gossipsub::{self, IdentTopic, MessageAuthenticity, MessageId, ValidationMode},
    identity,
    noise,
    swarm::SwarmEvent,
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

/// Build a libp2p Swarm with GossipSub configured for Filecoin interop.
pub fn build_swarm() -> Result<Swarm<gossipsub::Behaviour>, Box<dyn std::error::Error>> {
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

    let swarm = libp2p::SwarmBuilder::with_existing_identity(local_key)
        .with_tokio()
        .with_tcp(
            tcp::Config::default(),
            noise::Config::new,
            yamux::Config::default,
        )?
        .with_behaviour(|_| gossipsub)?
        .with_swarm_config(|c| c.with_idle_connection_timeout(Duration::from_secs(60)))
        .build();

    Ok(swarm)
}

/// Run the network event loop. Connects to peers, subscribes to topics, and
/// publishes messages received on the `rx` channel.
pub async fn run_network(
    mut swarm: Swarm<gossipsub::Behaviour>,
    peers: Vec<(Multiaddr, PeerId)>,
    topics: Vec<String>,
    mut rx: mpsc::Receiver<PublishRequest>,
) {
    // Listen on random port
    swarm
        .listen_on("/ip4/0.0.0.0/tcp/0".parse().unwrap())
        .expect("failed to listen");

    // Dial all peers
    for (addr, peer_id) in &peers {
        let dial_addr = addr.clone().with(libp2p::multiaddr::Protocol::P2p(*peer_id));
        match swarm.dial(dial_addr.clone()) {
            Ok(_) => info!("dialing {}", dial_addr),
            Err(e) => warn!("failed to dial {}: {}", dial_addr, e),
        }
    }

    // Subscribe to topics
    for topic_str in &topics {
        let topic = IdentTopic::new(topic_str);
        if let Err(e) = swarm.behaviour_mut().subscribe(&topic) {
            warn!("failed to subscribe to {}: {}", topic_str, e);
        } else {
            info!("subscribed to {}", topic_str);
        }
    }

    info!("waiting 5s for GossipSub mesh formation...");
    let mesh_wait = tokio::time::sleep(Duration::from_secs(5));
    tokio::pin!(mesh_wait);

    loop {
        tokio::select! {
            _ = &mut mesh_wait => {}
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
                match swarm.behaviour_mut().publish(topic, req.data) {
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
