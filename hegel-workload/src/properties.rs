use log::info;

/// Log a generation event. In Antithesis mode, the hegeltest crate automatically
/// emits assertions to sdk.jsonl — this function provides additional logging.
pub fn log_generation(topic: &str, data_len: usize) {
    info!("generated {} bytes for topic {}", data_len, topic);
}
