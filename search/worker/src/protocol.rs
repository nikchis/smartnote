//! Protocol between Smartnote and the worker over stdin/stdout (spec §7): length-prefixed
//! frames whose bodies are CBOR maps with the message type in `t`.

pub mod frame;
pub mod messages;

pub use frame::{FrameError, MAX_FRAME, read_frame, write_frame};
pub use messages::{
    ErrorCode, Hit, MessageError, NoteIn, PROTO, RemovalIn, Reply, Request, Source, State, decode,
    encode,
};
