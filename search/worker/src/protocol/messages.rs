//! Messages of the protocol (spec §7.2) and their CBOR encoding.
//!
//! Byte layout, shared with Go through `search/testdata/protocol/*.cbor`: a definite-length map,
//! the tag `t` first, then the fields in declaration order; integers and floats in their
//! shortest lossless form; byte fields as CBOR byte strings. An optional field that is absent
//! (`NoteIn::emb`, `Reply::Error::req`) is omitted from the map, not written as null.

use serde::{Deserialize, Serialize};
use serde_bytes::ByteBuf;

use crate::sandbox::SandboxLevel;

/// Protocol version; Smartnote and the worker ship together and require an exact match.
pub const PROTO: u32 = 1;

/// Nesting limit of the decoder, the same as `MaxNestedLevels` of the Go side.
const MAX_NESTING: usize = 16;

/// Message from Smartnote to the worker.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "t", rename_all = "snake_case", deny_unknown_fields)]
pub enum Request {
    /// First message: version check and worker settings; answered by [`Reply::Ready`].
    Hello {
        proto: u32,
        model_dir: String,
        ort_lib: String,
        threads: u16,
        parent_pid: u32,
    },
    /// Drops the index; answered by [`Reply::Ok`].
    Reset { req: u64 },
    /// Adds or replaces notes (up to 100 per message); answered by [`Reply::Ok`].
    Upsert { req: u64, notes: Vec<NoteIn> },
    /// Removes notes; answered by [`Reply::Ok`].
    Remove { req: u64, items: Vec<RemovalIn> },
    /// Searches; answered by [`Reply::Result`].
    Search { req: u64, query: String, limit: u16 },
}

/// A note to index.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct NoteIn {
    /// UUID in lower case.
    pub id: String,
    /// Global change number; a lower `rev` than the indexed one is stale.
    pub rev: u64,
    pub name: String,
    pub info: String,
    /// Stored embedding record of the note (spec §4.2), omitted when there is none.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub emb: Option<ByteBuf>,
}

/// A note to remove.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct RemovalIn {
    /// UUID in lower case.
    pub id: String,
    pub rev: u64,
}

/// Message from the worker to Smartnote.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(tag = "t", rename_all = "snake_case", deny_unknown_fields)]
pub enum Reply {
    /// Answer to [`Request::Hello`].
    Ready {
        /// Worker build identity.
        worker: String,
        /// Index version id: changes when the index or embedding format changes.
        index: ByteBuf,
        lexical: State,
        semantic: State,
        sandbox: SandboxLevel,
    },
    /// Answer to `reset`, `upsert` and `remove`.
    Ok { req: u64 },
    /// Answer to [`Request::Search`].
    Result { req: u64, hits: Vec<Hit> },
    /// A new embedding record of a note, for Smartnote to store.
    Embedded { id: String, rev: u64, emb: ByteBuf },
    /// Progress of indexing.
    Status {
        notes: u32,
        queued: u32,
        lexical: State,
        semantic: State,
    },
    /// Failure of the request `req`, or of the worker when `req` is absent. `msg` never
    /// contains note or query text.
    Error {
        #[serde(default, skip_serializing_if = "Option::is_none")]
        req: Option<u64>,
        code: ErrorCode,
        msg: String,
    },
}

/// State of one kind of search.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum State {
    Loading,
    Ready,
    Unavailable,
}

/// Error code of [`Reply::Error`].
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ErrorCode {
    BadRequest,
    UnsupportedProto,
    ModelUnavailable,
    Internal,
}

/// One search result (spec §3.2 `SearchHit`). Offsets are UTF-16 code units of the source
/// text: within the name for line 0, within the whole `info` otherwise.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Hit {
    /// Note id.
    pub note: String,
    /// 0 for the name, 1…n for the lines of `info`.
    pub line: u32,
    pub start: u32,
    pub end: u32,
    /// Exact text of `[start, end)`, to check that the note has not changed.
    pub text: String,
    pub snippet: String,
    /// Highlight ranges `[start, end)` in the snippet, UTF-16.
    pub marks: Vec<[u32; 2]>,
    pub sources: Vec<Source>,
    pub score: f32,
}

/// Kind of search that found a hit.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Source {
    Words,
    Substring,
    Semantic,
}

/// Failure to decode or encode a message body. Carries no message content: decoder messages
/// may quote the offending value, which can be note or query text.
#[derive(Debug, PartialEq, Eq, thiserror::Error)]
pub enum MessageError {
    /// The body is not well-formed CBOR, or ends early.
    #[error("malformed CBOR at byte {0}")]
    Syntax(usize),
    /// Well-formed CBOR that is no valid message: unknown type, missing, unknown or mistyped
    /// field.
    #[error("not a valid message")]
    Semantic,
    /// Nesting deeper than the limit.
    #[error("nesting too deep")]
    TooDeep,
    /// Bytes after the message.
    #[error("bytes after the message")]
    Trailing,
    /// Encoding failed.
    #[error("encoding failed")]
    Encode,
}

/// Decodes a message body; the whole body must be one message.
///
/// # Errors
///
/// [`MessageError`] when the body is no valid message of type `T`.
pub fn decode<T: serde::de::DeserializeOwned>(body: &[u8]) -> Result<T, MessageError> {
    let mut rest = body;
    let msg =
        ciborium::de::from_reader_with_recursion_limit(&mut rest, MAX_NESTING).map_err(|err| {
            match err {
                // The reader is a slice: its only failure is the end of the body.
                ciborium::de::Error::Io(_) => MessageError::Syntax(body.len()),
                ciborium::de::Error::Syntax(at) => MessageError::Syntax(at),
                // The text of a semantic error may quote the value: dropped.
                ciborium::de::Error::Semantic(..) => MessageError::Semantic,
                ciborium::de::Error::RecursionLimitExceeded => MessageError::TooDeep,
            }
        })?;
    if rest.is_empty() {
        Ok(msg)
    } else {
        Err(MessageError::Trailing)
    }
}

/// Encodes a message body.
///
/// # Errors
///
/// [`MessageError::Encode`] when serialization fails (not expected for the protocol types).
pub fn encode<T: Serialize>(msg: &T) -> Result<Vec<u8>, MessageError> {
    let mut body = Vec::new();
    ciborium::ser::into_writer(msg, &mut body).map_err(|_| MessageError::Encode)?;
    Ok(body)
}
