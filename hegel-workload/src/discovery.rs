use libp2p::{Multiaddr, PeerId};
use log::{info, warn};
use std::time::Duration;

/// A discovered target node with its libp2p address and peer ID.
#[derive(Debug, Clone)]
pub struct TargetNode {
    pub name: String,
    pub addr: Multiaddr,
    pub peer_id: PeerId,
}

/// Parse a multiaddr string like "/ip4/.../tcp/.../p2p/<peer_id>" into (dial_addr, peer_id).
/// Returns the multiaddr without the /p2p/ suffix (for dialing) and the extracted PeerId.
pub fn parse_multiaddr(s: &str) -> Option<(Multiaddr, PeerId)> {
    let full: Multiaddr = s.parse().ok()?;
    let mut addr = Multiaddr::empty();
    let mut peer_id = None;
    for proto in full.iter() {
        match proto {
            libp2p::multiaddr::Protocol::P2p(id) => {
                peer_id = Some(id);
            }
            other => {
                addr.push(other);
            }
        }
    }
    Some((addr, peer_id?))
}

/// Discover nodes by reading multiaddr files from devgen directory.
/// Retries every 5 seconds for up to 5 minutes per node.
pub fn discover_nodes(names: &[String], devgen_dir: &str) -> Vec<TargetNode> {
    let mut nodes = Vec::new();
    for name in names {
        let path = format!("{}/{}/{}-ipv4addr", devgen_dir, name, name);
        info!("waiting for multiaddr file: {}", path);

        let mut content = None;
        for attempt in 0..60 {
            match std::fs::read_to_string(&path) {
                Ok(s) if !s.trim().is_empty() => {
                    content = Some(s);
                    break;
                }
                _ => {
                    if attempt % 12 == 0 {
                        info!("still waiting for {} (attempt {})", path, attempt);
                    }
                    std::thread::sleep(Duration::from_secs(5));
                }
            }
        }

        let Some(data) = content else {
            warn!("timed out waiting for {}, skipping", path);
            continue;
        };

        let line = data.trim();
        match parse_multiaddr(line) {
            Some((addr, peer_id)) => {
                info!("discovered {} at {} (peer {})", name, addr, peer_id);
                nodes.push(TargetNode {
                    name: name.clone(),
                    addr,
                    peer_id,
                });
            }
            None => {
                warn!("failed to parse multiaddr for {}: {:?}", name, line);
            }
        }
    }
    nodes
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_multiaddr_line() {
        let line = "/ip4/172.18.0.6/tcp/37081/p2p/12D3KooWKDdkB19Qi5S5Kq69dPfczv1Zdx1dj4ivsgLNKkKtWx5a";
        let (addr, peer_id) = parse_multiaddr(line).unwrap();
        assert!(addr.to_string().contains("172.18.0.6"));
        assert!(!peer_id.to_string().is_empty());
    }

    #[test]
    fn test_parse_multiaddr_with_whitespace() {
        let line = "/ip4/10.0.0.1/tcp/1234/p2p/12D3KooWKDdkB19Qi5S5Kq69dPfczv1Zdx1dj4ivsgLNKkKtWx5a";
        let result = parse_multiaddr(line.trim());
        assert!(result.is_some());
    }

    #[test]
    fn test_parse_multiaddr_invalid() {
        assert!(parse_multiaddr("not-a-multiaddr").is_none());
        assert!(parse_multiaddr("").is_none());
    }

    #[test]
    fn test_parse_multiaddr_no_peer_id() {
        // Multiaddr without /p2p/ component should return None
        assert!(parse_multiaddr("/ip4/10.0.0.1/tcp/1234").is_none());
    }
}
