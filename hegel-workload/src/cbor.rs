/// Write a CBOR major type header into a Vec<u8>.
fn write_major_type(buf: &mut Vec<u8>, major: u8, value: u64) {
    let mt = major << 5;
    if value < 24 {
        buf.push(mt | value as u8);
    } else if value < 256 {
        buf.push(mt | 24);
        buf.push(value as u8);
    } else if value < 65536 {
        buf.push(mt | 25);
        buf.extend_from_slice(&(value as u16).to_be_bytes());
    } else if value < 4_294_967_296 {
        buf.push(mt | 26);
        buf.extend_from_slice(&(value as u32).to_be_bytes());
    } else {
        buf.push(mt | 27);
        buf.extend_from_slice(&value.to_be_bytes());
    }
}

pub fn cbor_uint64(v: u64) -> Vec<u8> {
    let mut buf = Vec::new();
    write_major_type(&mut buf, 0, v);
    buf
}

pub fn cbor_int64(v: i64) -> Vec<u8> {
    if v >= 0 {
        cbor_uint64(v as u64)
    } else {
        let mut buf = Vec::new();
        write_major_type(&mut buf, 1, (-v - 1) as u64);
        buf
    }
}

pub fn cbor_bytes(b: &[u8]) -> Vec<u8> {
    let mut buf = Vec::new();
    write_major_type(&mut buf, 2, b.len() as u64);
    buf.extend_from_slice(b);
    buf
}

pub fn cbor_text(s: &str) -> Vec<u8> {
    let mut buf = Vec::new();
    write_major_type(&mut buf, 3, s.len() as u64);
    buf.extend_from_slice(s.as_bytes());
    buf
}

pub fn cbor_nil() -> Vec<u8> {
    vec![0xf6]
}

pub fn cbor_bool(v: bool) -> Vec<u8> {
    if v {
        vec![0xf5]
    } else {
        vec![0xf4]
    }
}

pub fn cbor_array(elements: &[&[u8]]) -> Vec<u8> {
    let mut buf = Vec::new();
    write_major_type(&mut buf, 4, elements.len() as u64);
    for e in elements {
        buf.extend_from_slice(e);
    }
    buf
}

pub fn cbor_cid(cid_bytes: &[u8]) -> Vec<u8> {
    let mut buf = Vec::new();
    write_major_type(&mut buf, 6, 42); // CBOR tag 42
    let tagged_len = cid_bytes.len() + 1;
    write_major_type(&mut buf, 2, tagged_len as u64);
    buf.push(0x00); // multibase identity prefix
    buf.extend_from_slice(cid_bytes);
    buf
}

pub fn big_int_bytes(v: u64) -> Vec<u8> {
    if v == 0 {
        return vec![];
    }
    let raw = v.to_be_bytes();
    let start = raw.iter().position(|&b| b != 0).unwrap_or(7);
    let mut result = Vec::with_capacity(1 + raw.len() - start);
    result.push(0x00); // positive sign
    result.extend_from_slice(&raw[start..]);
    result
}

pub fn random_cid() -> Vec<u8> {
    use sha2::{Digest, Sha256};
    let data: Vec<u8> = (0..32).map(|_| rand_byte()).collect();
    let hash = Sha256::digest(&data);
    let mut cid = Vec::new();
    cid.push(0x01); // CIDv1
    cid.push(0x71); // dag-cbor codec
    cid.push(0x12); // sha2-256 hash function
    cid.push(0x20); // 32-byte digest
    cid.extend_from_slice(&hash);
    cid
}

fn rand_byte() -> u8 {
    use std::collections::hash_map::RandomState;
    use std::hash::{BuildHasher, Hasher};
    let s = RandomState::new();
    let mut h = s.build_hasher();
    h.write_u8(0);
    (h.finish() & 0xFF) as u8
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_cbor_uint64_zero() {
        assert_eq!(cbor_uint64(0), vec![0x00]);
    }

    #[test]
    fn test_cbor_uint64_small() {
        assert_eq!(cbor_uint64(23), vec![0x17]);
    }

    #[test]
    fn test_cbor_uint64_one_byte() {
        assert_eq!(cbor_uint64(24), vec![0x18, 0x18]);
    }

    #[test]
    fn test_cbor_nil() {
        assert_eq!(cbor_nil(), vec![0xf6]);
    }

    #[test]
    fn test_cbor_bool() {
        assert_eq!(cbor_bool(true), vec![0xf5]);
        assert_eq!(cbor_bool(false), vec![0xf4]);
    }

    #[test]
    fn test_cbor_bytes_empty() {
        assert_eq!(cbor_bytes(&[]), vec![0x40]);
    }

    #[test]
    fn test_cbor_bytes_data() {
        assert_eq!(cbor_bytes(&[1, 2, 3]), vec![0x43, 1, 2, 3]);
    }

    #[test]
    fn test_cbor_array_empty() {
        assert_eq!(cbor_array(&[]), vec![0x80]);
    }

    #[test]
    fn test_cbor_array_nested() {
        let elements: Vec<Vec<u8>> = vec![cbor_uint64(1), cbor_nil()];
        let refs: Vec<&[u8]> = elements.iter().map(|e| e.as_slice()).collect();
        assert_eq!(cbor_array(&refs), vec![0x82, 0x01, 0xf6]);
    }

    #[test]
    fn test_cbor_int64_positive() {
        assert_eq!(cbor_int64(0), cbor_uint64(0));
        assert_eq!(cbor_int64(10), cbor_uint64(10));
    }

    #[test]
    fn test_cbor_int64_negative() {
        assert_eq!(cbor_int64(-1), vec![0x20]);
        assert_eq!(cbor_int64(-10), vec![0x29]);
    }

    #[test]
    fn test_big_int_bytes_zero() {
        assert_eq!(big_int_bytes(0), vec![]);
    }

    #[test]
    fn test_big_int_bytes_positive() {
        assert_eq!(big_int_bytes(1), vec![0x00, 0x01]);
        assert_eq!(big_int_bytes(256), vec![0x00, 0x01, 0x00]);
    }

    #[test]
    fn test_cbor_text() {
        assert_eq!(cbor_text(""), vec![0x60]);
        assert_eq!(cbor_text("hi"), vec![0x62, b'h', b'i']);
    }

    #[test]
    fn test_cbor_cid() {
        let cid_bytes = vec![0x01, 0x71, 0x12, 0x20];
        let encoded = cbor_cid(&cid_bytes);
        // Tag 42: 0xd8 0x2a, then byte string of len 5 (4 + 1 prefix): 0x45, then 0x00 prefix
        assert_eq!(encoded[0], 0xd8);
        assert_eq!(encoded[1], 0x2a);
        assert_eq!(encoded[2], 0x45);
        assert_eq!(encoded[3], 0x00);
        assert_eq!(&encoded[4..], &cid_bytes[..]);
    }

    #[test]
    fn test_random_cid_length() {
        let cid = random_cid();
        // CIDv1 header (4 bytes) + 32-byte SHA-256 digest = 36 bytes
        assert_eq!(cid.len(), 36);
        assert_eq!(cid[0], 0x01); // CIDv1
        assert_eq!(cid[1], 0x71); // dag-cbor
        assert_eq!(cid[2], 0x12); // sha2-256
        assert_eq!(cid[3], 0x20); // 32 bytes
    }

    #[test]
    fn test_cbor_uint64_two_byte() {
        // 256 = 0x100, needs 2-byte encoding
        assert_eq!(cbor_uint64(256), vec![0x19, 0x01, 0x00]);
    }

    #[test]
    fn test_cbor_uint64_four_byte() {
        // 65536 = 0x10000, needs 4-byte encoding
        assert_eq!(cbor_uint64(65536), vec![0x1a, 0x00, 0x01, 0x00, 0x00]);
    }

    #[test]
    fn test_cbor_uint64_eight_byte() {
        let v: u64 = 4_294_967_296; // 0x100000000
        let encoded = cbor_uint64(v);
        assert_eq!(
            encoded,
            vec![0x1b, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00]
        );
    }
}
