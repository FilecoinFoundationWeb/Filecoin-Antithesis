mod cbor;
mod discovery;
mod generators;
mod network;

use log::info;

fn main() {
    env_logger::init();
    info!("hegel-workload starting");
}
