//! The stored embedding record of a note and the ids it is checked against (spec §4.2).

use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use super::chunk::ChunkParams;
use super::files::Manifest;
use super::model::DIM;
use crate::protocol;
use crate::text::NORMALIZATION_VERSION;

/// Version of the record format (`"v"`); part of the index version id.
pub const RECORD_VERSION: u32 = 1;

/// Length of an encoded vector: [`DIM`] `f32` values, little-endian.
const VEC_BYTES: usize = DIM * 4;

/// Embedding of one chunk.
#[derive(Clone, Debug, PartialEq)]
pub struct ChunkVec {
    /// First line of the chunk in `info`, from 1.
    pub first: u32,
    /// Last line of the chunk.
    pub last: u32,
    /// L2-normalized embedding.
    pub vec: [f32; DIM],
}

/// Embeddings of a note, valid for one index version and one text of the note.
#[derive(Clone, Debug, PartialEq)]
pub struct Record {
    /// [`index_id`] the embeddings were computed for.
    pub index: [u8; 32],
    /// [`text_hash`] of the note the embeddings were computed from.
    pub text: [u8; 32],
    /// Embeddings of the chunks in order.
    pub chunks: Vec<ChunkVec>,
}

/// The record on the wire. Fields are declared in the deterministic key order of RFC 8949
/// §4.2.1 (shorter encoded key first, then bytewise): `v`, `text`, `index`, `chunks`.
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct WireRecord {
    v: u32,
    #[serde(with = "serde_bytes")]
    text: [u8; 32],
    #[serde(with = "serde_bytes")]
    index: [u8; 32],
    chunks: Vec<WireChunk>,
}

/// A chunk on the wire, keys in the order `vec`, `last`, `first`.
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct WireChunk {
    #[serde(with = "serde_bytes")]
    vec: Vec<u8>,
    last: u32,
    first: u32,
}

impl Record {
    /// Encodes the record as deterministic CBOR (RFC 8949 §4.2.1): definite lengths, shortest
    /// integers, keys in canonical order, vectors as byte strings of `f32` little-endian.
    ///
    /// # Panics
    ///
    /// Never: encoding these types into memory cannot fail.
    #[must_use]
    pub fn encode(&self) -> Vec<u8> {
        let wire = WireRecord {
            v: RECORD_VERSION,
            text: self.text,
            index: self.index,
            chunks: self
                .chunks
                .iter()
                .map(|c| WireChunk {
                    vec: c.vec.iter().flat_map(|x| x.to_le_bytes()).collect(),
                    last: c.last,
                    first: c.first,
                })
                .collect(),
        };
        protocol::encode(&wire).expect("CBOR encoding into memory cannot fail")
    }

    /// Decodes a record produced by [`Record::encode`]; `None` for anything else: malformed or
    /// non-canonical CBOR, another version, wrong lengths, non-finite values, line ranges that
    /// are not `1 <= first <= last`.
    #[must_use]
    pub fn decode(b: &[u8]) -> Option<Record> {
        let wire: WireRecord = protocol::decode(b).ok()?;
        if wire.v != RECORD_VERSION {
            return None;
        }
        let chunks = wire
            .chunks
            .iter()
            .map(chunk_from_wire)
            .collect::<Option<Vec<_>>>()?;
        let record = Record {
            index: wire.index,
            text: wire.text,
            chunks,
        };
        // Only the canonical encoding is accepted: one record has one byte form.
        (record.encode() == b).then_some(record)
    }
}

fn chunk_from_wire(c: &WireChunk) -> Option<ChunkVec> {
    if c.vec.len() != VEC_BYTES || c.first == 0 || c.first > c.last {
        return None;
    }
    let mut vec = [0.0; DIM];
    let (values, _) = c.vec.as_chunks::<4>();
    for (x, &bytes) in vec.iter_mut().zip(values) {
        *x = f32::from_le_bytes(bytes);
        if !x.is_finite() {
            return None;
        }
    }
    Some(ChunkVec {
        first: c.first,
        last: c.last,
        vec,
    })
}

/// Id of the index version (spec §4.2): SHA-256 over a domain tag, [`RECORD_VERSION`], the
/// SHA-256 of `model.onnx` and `tokenizer.kit`, every field of `p` and
/// [`NORMALIZATION_VERSION`], all as fixed-width little-endian integers.
#[must_use]
pub fn index_id(m: &Manifest, p: &ChunkParams) -> [u8; 32] {
    const TAG: &[u8] = b"smartnote-search/index";
    let mut h = Sha256::new();
    h.update((TAG.len() as u64).to_le_bytes());
    h.update(TAG);
    h.update(RECORD_VERSION.to_le_bytes());
    h.update(m.onnx_sha256);
    h.update(m.kit_sha256);
    for value in [
        p.target,
        p.max,
        p.min_tail,
        p.overlap_lines,
        p.window,
        p.window_overlap,
    ] {
        h.update((value as u64).to_le_bytes());
    }
    h.update(NORMALIZATION_VERSION.to_le_bytes());
    h.finalize().into()
}

/// SHA-256 of `name + "\n" + info`.
#[must_use]
pub fn text_hash(name: &str, info: &str) -> [u8; 32] {
    let mut h = Sha256::new();
    h.update(name);
    h.update("\n");
    h.update(info);
    h.finalize().into()
}

#[cfg(test)]
mod tests {
    use sha2::{Digest, Sha256};

    use super::*;

    fn vector(seed: f32) -> [f32; DIM] {
        let mut v = [0.0; DIM];
        for (i, x) in v.iter_mut().enumerate() {
            #[allow(clippy::cast_precision_loss)]
            let i = i as f32;
            *x = (seed + i).sin() / 20.0;
        }
        v
    }

    fn record() -> Record {
        Record {
            index: [7; 32],
            text: [9; 32],
            chunks: vec![
                ChunkVec {
                    first: 1,
                    last: 12,
                    vec: vector(0.5),
                },
                ChunkVec {
                    first: 12,
                    last: 300,
                    vec: vector(-1.25),
                },
            ],
        }
    }

    fn manifest() -> Manifest {
        Manifest {
            onnx_sha256: [1; 32],
            kit_sha256: [2; 32],
        }
    }

    #[test]
    fn record_roundtrip_deterministic() {
        let r = record();

        let bytes = r.encode();
        let decoded = Record::decode(&bytes).expect("own encoding decodes");

        assert_eq!(decoded, r);
        assert_eq!(decoded.encode(), bytes);
    }

    #[test]
    fn record_keys_in_canonical_order() {
        let bytes = record().encode();

        // Map of 4 pairs; "v": 1; "text": bytes(32).
        let mut head = vec![0xa4, 0x61, b'v', 0x01, 0x64];
        head.extend_from_slice(b"text");
        head.extend_from_slice(&[0x58, 32]);
        assert_eq!(&bytes[..head.len()], head.as_slice());
        // "index": bytes(32) after the text hash; "chunks": array of 2.
        let at = head.len() + 32;
        let mut middle = vec![0x65];
        middle.extend_from_slice(b"index");
        middle.extend_from_slice(&[0x58, 32]);
        assert_eq!(&bytes[at..at + middle.len()], middle.as_slice());
        let at = at + middle.len() + 32;
        let mut chunks = vec![0x66];
        chunks.extend_from_slice(b"chunks");
        // Array of 2; the first chunk: map of 3 pairs, "vec": bytes(1536).
        chunks.extend_from_slice(&[0x82, 0xa3, 0x63, b'v', b'e', b'c', 0x59, 0x06, 0x00]);
        assert_eq!(&bytes[at..at + chunks.len()], chunks.as_slice());
        // "last": 12, "first": 1 after the vector.
        let at = at + chunks.len() + DIM * 4;
        let mut lines = vec![0x64];
        lines.extend_from_slice(b"last");
        lines.push(0x0c);
        lines.push(0x65);
        lines.extend_from_slice(b"first");
        lines.push(0x01);
        assert_eq!(&bytes[at..at + lines.len()], lines.as_slice());
    }

    #[test]
    fn record_vector_is_f32_little_endian() {
        let r = record();
        let bytes = r.encode();
        let expected: Vec<u8> = r.chunks[0]
            .vec
            .iter()
            .flat_map(|x| x.to_le_bytes())
            .collect();

        assert!(
            bytes
                .windows(expected.len())
                .any(|w| w == expected.as_slice())
        );
    }

    #[test]
    fn record_decode_rejects_garbage() {
        let good = record().encode();
        let mut cases: Vec<Vec<u8>> = vec![
            Vec::new(),
            vec![0xff],
            b"not cbor at all".to_vec(),
            good[..good.len() - 1].to_vec(),
            [good.as_slice(), &[0x00]].concat(),
        ];
        // Every single-bit change in the structure of the head: map, keys, "v" and the headers
        // of the byte strings (bytes 0..11 and 43..51), the "chunks" key and the first chunk
        // up to its vector (bytes 83..99). The hashes themselves may hold any value.
        for i in (0..11).chain(43..51).chain(83..99) {
            let mut bad = good.clone();
            bad[i] ^= 0x01;
            cases.push(bad);
        }
        // Every truncation of the head.
        for len in 0..120 {
            cases.push(good[..len].to_vec());
        }

        for case in &cases {
            assert_eq!(Record::decode(case), None, "{case:x?}");
        }
    }

    #[test]
    fn record_decode_rejects_wrong_version_lengths_and_values() {
        let mut wrong_version = record().encode();
        wrong_version[3] = 0x02;
        assert_eq!(Record::decode(&wrong_version), None);

        let mut nan = record();
        nan.chunks[1].vec[5] = f32::NAN;
        assert_eq!(Record::decode(&nan.encode()), None);

        let mut infinite = record();
        infinite.chunks[0].vec[0] = f32::INFINITY;
        assert_eq!(Record::decode(&infinite.encode()), None);

        let mut reversed = record();
        reversed.chunks[0].first = 13;
        assert_eq!(Record::decode(&reversed.encode()), None);

        let mut zero_line = record();
        zero_line.chunks[0].first = 0;
        assert_eq!(Record::decode(&zero_line.encode()), None);

        // A vector of 383 values: the byte string is 1532 bytes long.
        let short = record().encode();
        let at = short
            .windows(3)
            .position(|w| w == [0x59, 0x06, 0x00])
            .expect("vec header");
        let mut short_vec = short[..at].to_vec();
        short_vec.extend_from_slice(&[0x59, 0x05, 0xfc]);
        short_vec.extend_from_slice(&short[at + 3..at + 3 + 1532]);
        short_vec.extend_from_slice(&short[at + 3 + 1536..]);
        assert_eq!(Record::decode(&short_vec), None);
    }

    #[test]
    fn record_without_chunks_roundtrips() {
        let r = Record {
            index: [0; 32],
            text: [0xff; 32],
            chunks: Vec::new(),
        };

        assert_eq!(Record::decode(&r.encode()), Some(r));
    }

    #[test]
    fn index_id_changes_with_params_and_files() {
        let m = manifest();
        let p = ChunkParams::default();
        let base = index_id(&m, &p);

        assert_eq!(index_id(&m, &p), base, "deterministic");
        let other_target = ChunkParams { target: 192, ..p };
        assert_ne!(index_id(&m, &other_target), base);
        let other_overlap = ChunkParams {
            window_overlap: 16,
            ..p
        };
        assert_ne!(index_id(&m, &other_overlap), base);
        let other_onnx = Manifest {
            onnx_sha256: [3; 32],
            ..m.clone()
        };
        assert_ne!(index_id(&other_onnx, &p), base);
        let other_kit = Manifest {
            kit_sha256: [3; 32],
            ..m
        };
        assert_ne!(index_id(&other_kit, &p), base);
    }

    #[test]
    fn index_id_distinguishes_swapped_params() {
        let m = manifest();
        let p = ChunkParams::default();
        let swapped = ChunkParams {
            window: p.window_overlap,
            window_overlap: p.window,
            ..p
        };

        assert_ne!(index_id(&m, &swapped), index_id(&m, &p));
    }

    #[test]
    fn text_hash_of_name_and_info() {
        let expected: [u8; 32] = Sha256::digest("Заметка\nстрока 1\nстрока 2").into();

        assert_eq!(text_hash("Заметка", "строка 1\nстрока 2"), expected);
        assert_ne!(text_hash("a", "b"), text_hash("a", "c"));
    }
}
