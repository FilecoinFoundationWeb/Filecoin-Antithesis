mod cbor;
mod discovery;
mod generators;
mod network;
mod properties;

use log::info;

fn main() {
    env_logger::init();
    info!("hegel-workload starting");
}
