//! Messages against the shared samples `search/testdata/protocol/*.cbor` (spec §12.2): the Go
//! side decodes and re-encodes the same files byte for byte.
//!
//! `UPDATE_GOLDEN=1 cargo test -p worker --test protocol` rewrites the samples from the values
//! below; a normal run only compares.

use std::path::PathBuf;
use std::{env, fs};

use serde_bytes::ByteBuf;
use worker::protocol::{
    ErrorCode, Hit, MessageError, NoteIn, RemovalIn, Reply, Request, Source, State, decode, encode,
};
use worker::sandbox::SandboxLevel;

const NOTE_A: &str = "6f1c2a9e-3b4d-4e5f-8a7b-9c0d1e2f3a4b";
const NOTE_B: &str = "0b8e7d6c-5a4f-4e3d-9c2b-1a0f9e8d7c6b";

/// Deterministic bytes that look like an embedding record.
fn bytes(len: u8, seed: u8) -> ByteBuf {
    ByteBuf::from(
        (0..len)
            .map(|i| i.wrapping_mul(37) ^ seed)
            .collect::<Vec<u8>>(),
    )
}

enum Message {
    Request(Request),
    Reply(Reply),
}

fn samples() -> Vec<(&'static str, Message)> {
    let mut all = request_samples();
    all.extend(reply_samples());
    all
}

fn request_samples() -> Vec<(&'static str, Message)> {
    vec![
        (
            "hello",
            Message::Request(Request::Hello {
                proto: 1,
                model_dir: "/home/user/.local/share/smartnote/model/e5-small".into(),
                ort_lib: "/opt/smartnote/lib/libonnxruntime.so.1.22.0".into(),
                threads: 4,
                parent_pid: 41_234,
            }),
        ),
        (
            "upsert_with_emb",
            Message::Request(Request::Upsert {
                req: 7,
                notes: vec![
                    NoteIn {
                        id: NOTE_A.into(),
                        rev: 1042,
                        name: "Ёжик".into(),
                        info: "Первая строка\nretry backoff -> 2s\n😀".into(),
                        emb: Some(bytes(40, 0x5a)),
                    },
                    NoteIn {
                        id: NOTE_B.into(),
                        rev: 1043,
                        name: "Plan".into(),
                        info: String::new(),
                        emb: None,
                    },
                ],
            }),
        ),
        (
            "remove",
            Message::Request(Request::Remove {
                req: 8,
                items: vec![
                    RemovalIn {
                        id: NOTE_A.into(),
                        rev: 1044,
                    },
                    RemovalIn {
                        id: NOTE_B.into(),
                        rev: 70_000,
                    },
                ],
            }),
        ),
        (
            "search",
            Message::Request(Request::Search {
                req: 9,
                query: "\"retry backoff\" ->".into(),
                limit: 50,
            }),
        ),
    ]
}

fn reply_samples() -> Vec<(&'static str, Message)> {
    vec![
        (
            "ready",
            Message::Reply(Reply::Ready {
                worker: "smartnote-search 2.1.0".into(),
                index: bytes(32, 0xc3),
                lexical: State::Ready,
                semantic: State::Loading,
                sandbox: SandboxLevel::Full,
            }),
        ),
        (
            "result",
            Message::Reply(Reply::Result {
                req: 9,
                hits: vec![
                    Hit {
                        note: NOTE_A.into(),
                        line: 2,
                        start: 14,
                        end: 30,
                        text: "retry backoff ->".into(),
                        snippet: "retry backoff -> 2s".into(),
                        marks: vec![[0, 13], [14, 16]],
                        sources: vec![Source::Words, Source::Substring, Source::Semantic],
                        score: 0.032_786_883,
                    },
                    Hit {
                        note: NOTE_B.into(),
                        line: 0,
                        start: 0,
                        end: 4,
                        text: "Plan".into(),
                        snippet: "Plan".into(),
                        marks: vec![],
                        sources: vec![Source::Semantic],
                        score: 0.016_393_442,
                    },
                ],
            }),
        ),
        (
            "result_f16",
            Message::Reply(Reply::Result {
                req: 10,
                hits: vec![Hit {
                    note: NOTE_B.into(),
                    line: 1,
                    start: 6,
                    end: 8,
                    text: "==".into(),
                    snippet: "if (a == b)".into(),
                    marks: vec![[6, 8]],
                    sources: vec![Source::Substring],
                    // Exact in f16, so written as a 2-byte float: 0xf9 0x38 0x00.
                    score: 0.5,
                }],
            }),
        ),
        (
            "embedded",
            Message::Reply(Reply::Embedded {
                id: NOTE_A.into(),
                rev: 1042,
                emb: bytes(40, 0x5a),
            }),
        ),
        (
            "status",
            Message::Reply(Reply::Status {
                notes: 1000,
                queued: 37,
                lexical: State::Ready,
                semantic: State::Unavailable,
            }),
        ),
        (
            "error",
            Message::Reply(Reply::Error {
                req: Some(9),
                code: ErrorCode::BadRequest,
                msg: "limit out of range".into(),
            }),
        ),
        (
            "error_no_req",
            Message::Reply(Reply::Error {
                req: None,
                code: ErrorCode::BadRequest,
                msg: "malformed frame".into(),
            }),
        ),
    ]
}

fn golden_path(name: &str) -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../testdata/protocol")
        .join(format!("{name}.cbor"))
}

#[test]
fn golden_files_roundtrip() {
    let update = env::var_os("UPDATE_GOLDEN").is_some_and(|v| v == "1");
    for (name, msg) in samples() {
        let path = golden_path(name);
        let expected = match &msg {
            Message::Request(m) => encode(m),
            Message::Reply(m) => encode(m),
        }
        .unwrap_or_else(|e| panic!("{name}: encode: {e}"));
        if update {
            fs::write(&path, &expected).unwrap_or_else(|e| panic!("{name}: write: {e}"));
        }
        let stored = fs::read(&path).unwrap_or_else(|e| panic!("{name}: read {path:?}: {e}"));

        let reencoded = match &msg {
            Message::Request(m) => {
                let decoded: Request = decode(&stored).unwrap_or_else(|e| panic!("{name}: {e}"));
                assert_eq!(&decoded, m, "{name}: decoded value");
                encode(&decoded)
            }
            Message::Reply(m) => {
                let decoded: Reply = decode(&stored).unwrap_or_else(|e| panic!("{name}: {e}"));
                assert_eq!(&decoded, m, "{name}: decoded value");
                encode(&decoded)
            }
        }
        .unwrap_or_else(|e| panic!("{name}: re-encode: {e}"));
        assert_eq!(reencoded, stored, "{name}: re-encoded bytes");
        assert_eq!(expected, stored, "{name}: encoding of the sample value");
    }
}

#[test]
fn golden_layout_tag_first_absent_options_omitted() {
    let stored = fs::read(golden_path("upsert_with_emb")).expect("sample");
    // Definite map of 3 pairs, then the text "t" and the text "upsert".
    assert_eq!(&stored[..10], b"\xa3\x61t\x66upsert");
    // The second note has 4 pairs: `emb` is omitted, not null.
    let second = stored.windows(4).filter(|w| w == b"\xa4\x62id").count();
    assert_eq!(second, 1, "one note map without emb");

    let error = encode(&Reply::Error {
        req: None,
        code: ErrorCode::Internal,
        msg: String::new(),
    })
    .expect("encode");
    assert_eq!(error, b"\xa3\x61t\x65error\x64code\x68internal\x63msg\x60");

    // `score` 0.5 is exact in f16: the shortest lossless float, 0xf9 0x3800.
    let f16 = fs::read(golden_path("result_f16")).expect("sample");
    assert!(f16.ends_with(b"\x65score\xf9\x38\x00"), "{f16:x?}");
    // Absent `req`: a map of 3 pairs without the key.
    let no_req = fs::read(golden_path("error_no_req")).expect("sample");
    assert_eq!(&no_req[..9], b"\xa3\x61t\x65error");
    assert!(!no_req.windows(4).any(|w| w == b"\x63req"));
}

#[test]
fn absent_options_accept_null() {
    // {"t":"error","req":null,"code":"internal","msg":""}
    let body = b"\xa4\x61t\x65error\x63req\xf6\x64code\x68internal\x63msg\x60";
    let decoded: Reply = decode(body).expect("null req");
    assert_eq!(
        decoded,
        Reply::Error {
            req: None,
            code: ErrorCode::Internal,
            msg: String::new()
        }
    );
}

#[test]
fn unknown_type_rejected() {
    // {"t":"nope"}
    let body = b"\xa1\x61t\x64nope";
    assert_eq!(decode::<Request>(body), Err(MessageError::Semantic));
    assert_eq!(decode::<Reply>(body), Err(MessageError::Semantic));
}

#[test]
fn unknown_field_rejected() {
    // {"t":"reset","req":1,"x":0}
    let body = b"\xa3\x61t\x65reset\x63req\x01\x61x\x00";
    assert_eq!(decode::<Request>(body), Err(MessageError::Semantic));
}

#[test]
fn missing_field_rejected() {
    // {"t":"reset"}
    assert_eq!(
        decode::<Request>(b"\xa1\x61t\x65reset"),
        Err(MessageError::Semantic)
    );
}

#[test]
fn trailing_bytes_rejected() {
    // {"t":"reset","req":1} followed by 0x00
    let body = b"\xa2\x61t\x65reset\x63req\x01\x00";
    assert_eq!(
        decode::<Request>(&body[..body.len() - 1]),
        Ok(Request::Reset { req: 1 })
    );
    assert_eq!(decode::<Request>(body), Err(MessageError::Trailing));
}

#[test]
fn malformed_and_truncated_rejected() {
    assert!(matches!(
        decode::<Request>(b"\xa2\x61t\x65res"),
        Err(MessageError::Syntax(_))
    ));
    // Reserved additional information 28.
    assert!(matches!(
        decode::<Request>(b"\x1c"),
        Err(MessageError::Syntax(_))
    ));
    // A lone break code is well-formed bytes but no item.
    assert!(decode::<Request>(b"\xff").is_err());
    assert!(matches!(
        decode::<Request>(b""),
        Err(MessageError::Syntax(_))
    ));
}

#[test]
fn deep_nesting_rejected() {
    // {"t":"reset","req":[[[ ... ]]]} with 100 levels.
    let mut body = b"\xa2\x61t\x65reset\x63req".to_vec();
    body.extend(std::iter::repeat_n(0x81, 100));
    body.push(0x00);
    assert_eq!(decode::<Request>(&body), Err(MessageError::TooDeep));
}

#[test]
fn errors_do_not_quote_content() {
    // {"t":"search","req":"СЕКРЕТ","query":"q","limit":1}: a mistyped field with note text.
    let mut body = b"\xa4\x61t\x66search\x63req\x6c".to_vec();
    body.extend_from_slice("СЕКРЕТ".as_bytes());
    body.extend_from_slice(b"\x65query\x61q\x65limit\x01");
    let err = decode::<Request>(&body).expect_err("mistyped req");
    assert!(!format!("{err} {err:?}").contains("СЕКРЕТ"), "{err:?}");
}
