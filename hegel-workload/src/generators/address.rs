use hegel::generators as gs;

/// Generate a Filecoin address as raw bytes.
#[hegel::composite]
pub fn filecoin_address(tc: hegel::TestCase) -> Vec<u8> {
    let protocol: u8 = tc.draw(gs::sampled_from(vec![0u8, 1, 2, 3, 4, 5, 0xff]));
    match protocol {
        0 => tc.draw(id_address()),
        1 => tc.draw(hash_address(1)),
        2 => tc.draw(hash_address(2)),
        3 => tc.draw(bls_address()),
        4 => tc.draw(delegated_address()),
        _ => {
            let len: usize = tc.draw(gs::integers::<usize>().min_value(0).max_value(20));
            let mut addr = vec![protocol];
            let payload: Vec<u8> =
                tc.draw(gs::vecs(gs::integers::<u8>()).min_size(len).max_size(len));
            addr.extend(payload);
            addr
        }
    }
}

/// ID address: protocol 0 + varint-encoded actor ID with edge cases.
#[hegel::composite]
pub fn id_address(tc: hegel::TestCase) -> Vec<u8> {
    let variant: u8 = tc.draw(gs::integers::<u8>().min_value(0).max_value(4));
    match variant {
        0 => vec![0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f], // overflow varint
        1 => {
            vec![
                0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01,
            ] // 10-byte varint
        }
        2 => vec![0x00, 0x80], // truncated varint
        3 => vec![0x00],       // empty payload
        _ => {
            let len: usize = tc.draw(gs::integers::<usize>().min_value(1).max_value(9));
            let mut addr = vec![0x00];
            let payload: Vec<u8> =
                tc.draw(gs::vecs(gs::integers::<u8>()).min_size(len).max_size(len));
            addr.extend(payload);
            addr
        }
    }
}

/// Hash address (secp256k1=1 or actor=2): 20-byte payload with fuzzed lengths.
#[hegel::composite]
pub fn hash_address(tc: hegel::TestCase, proto_byte: u8) -> Vec<u8> {
    let variant: u8 = tc.draw(gs::integers::<u8>().min_value(0).max_value(3));
    match variant {
        0 => {
            let len: usize = tc.draw(gs::integers::<usize>().min_value(0).max_value(40));
            let mut addr = vec![proto_byte];
            let payload: Vec<u8> =
                tc.draw(gs::vecs(gs::integers::<u8>()).min_size(len).max_size(len));
            addr.extend(payload);
            addr
        }
        1 => vec![proto_byte], // empty payload
        2 => {
            let mut addr = vec![proto_byte];
            let payload: Vec<u8> =
                tc.draw(gs::vecs(gs::integers::<u8>()).min_size(30).max_size(30));
            addr.extend(payload);
            addr
        }
        _ => {
            let mut addr = vec![proto_byte];
            let payload: Vec<u8> =
                tc.draw(gs::vecs(gs::integers::<u8>()).min_size(20).max_size(20));
            addr.extend(payload);
            addr
        }
    }
}

/// BLS address: protocol 3, 48-byte public key with varied lengths.
#[hegel::composite]
pub fn bls_address(tc: hegel::TestCase) -> Vec<u8> {
    let variant: u8 = tc.draw(gs::integers::<u8>().min_value(0).max_value(2));
    match variant {
        0 => {
            let len: usize = tc.draw(gs::integers::<usize>().min_value(0).max_value(60));
            let mut addr = vec![0x03];
            let payload: Vec<u8> =
                tc.draw(gs::vecs(gs::integers::<u8>()).min_size(len).max_size(len));
            addr.extend(payload);
            addr
        }
        1 => vec![0x03], // empty payload
        _ => {
            let mut addr = vec![0x03];
            let payload: Vec<u8> =
                tc.draw(gs::vecs(gs::integers::<u8>()).min_size(48).max_size(48));
            addr.extend(payload);
            addr
        }
    }
}

/// Delegated address: protocol 4, namespace varint + sub-address with edge cases.
#[hegel::composite]
pub fn delegated_address(tc: hegel::TestCase) -> Vec<u8> {
    let variant: u8 = tc.draw(gs::integers::<u8>().min_value(0).max_value(5));
    match variant {
        0 => vec![0x04, 0x0a],                                     // empty sub-address
        1 => vec![0x04, 0xff, 0xff, 0xff, 0xff, 0x0f],             // max namespace varint
        2 => vec![0x04, 0x80],                                     // truncated namespace varint
        3 => {
            let mut addr = vec![0x04, 0x0a]; // namespace 10
            let payload: Vec<u8> =
                tc.draw(gs::vecs(gs::integers::<u8>()).min_size(128).max_size(128));
            addr.extend(payload);
            addr
        }
        4 => {
            let mut addr = vec![0x04, 0x00]; // namespace 0 (invalid)
            let payload: Vec<u8> =
                tc.draw(gs::vecs(gs::integers::<u8>()).min_size(20).max_size(20));
            addr.extend(payload);
            addr
        }
        _ => {
            let len: usize = tc.draw(gs::integers::<usize>().min_value(0).max_value(40));
            let mut addr = vec![0x04, 0x0a]; // EAM namespace 10
            let payload: Vec<u8> =
                tc.draw(gs::vecs(gs::integers::<u8>()).min_size(len).max_size(len));
            addr.extend(payload);
            addr
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[hegel::test(test_cases = 50)]
    fn test_filecoin_address_produces_bytes(tc: hegel::TestCase) {
        let addr: Vec<u8> = tc.draw(filecoin_address());
        assert!(!addr.is_empty(), "address must not be empty");
        let proto = addr[0];
        assert!(
            proto <= 5 || proto == 0xff,
            "unexpected protocol byte: {}",
            proto
        );
    }

    #[hegel::test(test_cases = 50)]
    fn test_valid_id_address(tc: hegel::TestCase) {
        let addr: Vec<u8> = tc.draw(id_address());
        assert_eq!(addr[0], 0x00, "ID address protocol must be 0");
        assert!(
            addr.len() >= 1,
            "ID address must have at least protocol byte"
        );
    }

    #[hegel::test(test_cases = 50)]
    fn test_bls_address_protocol(tc: hegel::TestCase) {
        let addr: Vec<u8> = tc.draw(bls_address());
        assert_eq!(addr[0], 0x03, "BLS address protocol must be 3");
    }
}
