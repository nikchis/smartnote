//! Semantic part of the index (spec §4.2, §6.7, §6.9): verified model files and ONNX Runtime
//! library, the e5-small model, chunking of notes and the stored embedding records.

pub mod chunk;
pub mod files;
pub mod model;
pub mod record;

pub use chunk::{ChunkParams, ChunkSpan, Chunks, chunk_note};
pub use files::{Manifest, ModelFiles, OrtLibrary, has_avx2};
pub use model::{DIM, DISABLE_TELEMETRY, Model, load_ort};
pub use record::{ChunkVec, RECORD_VERSION, Record, index_id, text_hash};

/// Failure to verify or load the model, or to compute an embedding. Carries no note text.
#[derive(Debug, thiserror::Error)]
pub enum ModelError {
    /// Reading a file failed.
    #[error("reading a model file failed")]
    Io(#[from] std::io::Error),
    /// The SHA-256 of a file differs from the digest pinned at build time.
    #[error("SHA-256 of {file} differs from the pinned digest")]
    Hash {
        /// Name of the file (`model.onnx`, `tokenizer.kit`, `manifest.json`, `onnxruntime`).
        file: &'static str,
    },
    /// `manifest.json` is malformed or names other file digests.
    #[error("manifest.json is invalid")]
    Manifest,
    /// The ONNX Runtime library path is relative: only absolute paths are loaded.
    #[error("the ONNX Runtime library path is not absolute")]
    RelativePath,
    /// No ONNX Runtime library is pinned for the build target.
    #[error("no ONNX Runtime library is pinned for this target")]
    UnsupportedTarget,
    /// Loading the ONNX Runtime library failed.
    #[error("loading the ONNX Runtime library failed")]
    Load(#[source] ort::LoadDynamicError),
    /// `ORT_DISABLE_TELEMETRY` is not `1`: [`load_ort`] refuses to start ONNX Runtime.
    #[error("ORT_DISABLE_TELEMETRY is not 1")]
    TelemetryEnabled,
    /// [`load_ort`] has not succeeded before [`Model::new`].
    #[error("the ONNX Runtime library is not loaded")]
    NotLoaded,
    /// The tokenizer file is invalid or failed to encode a text.
    #[error("tokenizer failed")]
    Tokenizer,
    /// ONNX Runtime failed.
    #[error("ONNX Runtime failed")]
    Ort(#[source] ort::Error),
    /// The model has unexpected inputs or outputs, or produced a degenerate embedding.
    #[error("unexpected model inputs or outputs")]
    Output,
}

impl<R> From<ort::Error<R>> for ModelError
where
    ort::Error: From<ort::Error<R>>,
{
    fn from(err: ort::Error<R>) -> Self {
        Self::Ort(err.into())
    }
}
