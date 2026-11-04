//! The e5-small model: `kitoken` tokenizer and an ONNX Runtime session (spec §5.1, §6.9).

use std::env;
use std::ffi::OsStr;
use std::sync::{Arc, Mutex, PoisonError};

use kitoken::Kitoken;
use ort::environment::Environment;
use ort::logging::LogLevel;
use ort::session::Session;
use ort::value::{Outlet, TensorRef};
use unicode_normalization::UnicodeNormalization;

use super::ModelError;
use super::files::{ModelFiles, OrtLibrary};

/// Dimension of an embedding.
pub const DIM: usize = 384;

/// Longest model input, special tokens included.
const MAX_TOKENS: usize = 512;
/// `<s>` and `</s>` of the XLM-R vocabulary, around every input.
const BOS: i64 = 0;
const EOS: i64 = 2;

const INPUT_IDS: &str = "input_ids";
const ATTENTION_MASK: &str = "attention_mask";
const TOKEN_TYPE_IDS: &str = "token_type_ids";
const HIDDEN_STATE: &str = "last_hidden_state";

/// Variable that keeps ONNX Runtime from starting its telemetry client; see [`load_ort`].
pub const DISABLE_TELEMETRY: &str = "ORT_DISABLE_TELEMETRY";

/// Whether the value of [`DISABLE_TELEMETRY`] disables the telemetry: exactly `1`.
fn telemetry_disabled(value: Option<&OsStr>) -> bool {
    value == Some(OsStr::new("1"))
}

/// The process-wide ONNX Runtime environment, set by [`load_ort`].
static ENVIRONMENT: Mutex<Option<Environment>> = Mutex::new(None);

/// Loads the verified ONNX Runtime library and creates its environment, once per process:
/// telemetry off, log level errors only. Call it only when [`super::has_avx2`] is true.
/// Later calls return `Ok(())` without loading anything.
///
/// ONNX Runtime logs through [`log_to_stderr`]: errors only, whatever `ORT_LOG` says, and
/// without their text (spec §6.2: stderr gets codes only).
///
/// The process must have `ORT_DISABLE_TELEMETRY=1` in its environment, set before any thread
/// starts (the worker does it first thing in `main`): the official Linux build 1.31 otherwise
/// starts its telemetry client (1DS) when the environment is created, whatever
/// `DisableTelemetryEvents` says later. It reads `/proc/self/cgroup` and writes a device id
/// and an SQLite store under `~/.cache/Microsoft/DeveloperTools`.
///
/// # Errors
///
/// [`ModelError::TelemetryEnabled`] when `ORT_DISABLE_TELEMETRY` is not `1` (nothing is
/// loaded), [`ModelError::Load`] when the library cannot be loaded, [`ModelError::Ort`] when
/// the environment cannot be created.
pub fn load_ort(lib: &OrtLibrary) -> Result<(), ModelError> {
    if !telemetry_disabled(env::var_os(DISABLE_TELEMETRY).as_deref()) {
        return Err(ModelError::TelemetryEnabled);
    }
    let mut slot = ENVIRONMENT.lock().unwrap_or_else(PoisonError::into_inner);
    if slot.is_some() {
        return Ok(());
    }
    let env = ort::init_from(lib.path())
        .map_err(ModelError::Load)?
        .with_name("smartnote-search")
        .with_telemetry(false)
        .with_logger(Arc::new(log_to_stderr))
        .build()?;
    // A custom logger starts at the verbose level: errors only from here on.
    env.set_log_level(LogLevel::Error);
    *slot = Some(env);
    Ok(())
}

/// Logger of ONNX Runtime: an error or a fatal message becomes `onnxruntime <level> at
/// <source location>`; lower levels are dropped. The message text is dropped too: it may
/// quote values of the run.
fn log_to_stderr(level: LogLevel, _category: &str, _id: &str, location: &str, _message: &str) {
    if let Some(line) = log_line(level, location) {
        eprintln!("{line}");
    }
}

fn log_line(level: LogLevel, location: &str) -> Option<String> {
    let name = match level {
        LogLevel::Verbose | LogLevel::Info | LogLevel::Warning => return None,
        LogLevel::Error => "error",
        LogLevel::Fatal => "fatal",
    };
    Some(format!("onnxruntime {name} at {location}"))
}

/// The embedding model.
pub struct Model {
    tokenizer: Kitoken,
    session: Session,
    /// The graph takes `token_type_ids` (all zero for one sequence).
    token_types: bool,
}

impl Model {
    /// Creates the tokenizer and the ONNX Runtime session from the files in memory, with
    /// `threads` intra-op threads (at least 1, no spinning between runs); the file buffers are
    /// released afterwards.
    ///
    /// # Errors
    ///
    /// [`ModelError::NotLoaded`] before [`load_ort`], [`ModelError::Tokenizer`] for an invalid
    /// tokenizer, [`ModelError::Ort`] when the session cannot be created,
    /// [`ModelError::Output`] when the model has unexpected inputs or outputs.
    pub fn new(files: ModelFiles, threads: u16) -> Result<Model, ModelError> {
        let env = ENVIRONMENT
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .clone()
            .ok_or(ModelError::NotLoaded)?;
        let tokenizer = Kitoken::from_slice(&files.kit).map_err(|_| ModelError::Tokenizer)?;
        let mut builder = Session::builder(&env)?
            .with_intra_threads(usize::from(threads.max(1)))?
            .with_intra_op_spinning(false)?;
        let session = builder.commit_from_memory(&files.onnx)?;
        // The model buffer is no longer needed; the worker's allocator zeroes it on release.
        drop(files);

        let inputs: Vec<&str> = session.inputs().iter().map(Outlet::name).collect();
        let known = |name: &&str| [INPUT_IDS, ATTENTION_MASK, TOKEN_TYPE_IDS].contains(name);
        let has_output = session.outputs().iter().any(|o| o.name() == HIDDEN_STATE);
        if !inputs.contains(&INPUT_IDS)
            || !inputs.contains(&ATTENTION_MASK)
            || !inputs.iter().all(known)
            || !has_output
        {
            return Err(ModelError::Output);
        }
        let token_types = inputs.contains(&TOKEN_TYPE_IDS);
        Ok(Model {
            tokenizer,
            session,
            token_types,
        })
    }

    /// Number of tokens of `text` (after NFC) without special tokens; 0 when the tokenizer
    /// fails.
    #[must_use]
    pub fn count_tokens(&self, text: &str) -> usize {
        self.tokenize(text).map_or(0, |ids| ids.len())
    }

    /// Embedding of a chunk of a note: `passage: {name}\n{body}`.
    ///
    /// # Errors
    ///
    /// [`ModelError::Tokenizer`], [`ModelError::Ort`] or [`ModelError::Output`].
    pub fn embed_passage(&mut self, name: &str, body: &str) -> Result<[f32; DIM], ModelError> {
        self.embed(&format!("passage: {name}\n{body}"))
    }

    /// Embedding of a query: `query: {q}`.
    ///
    /// # Errors
    ///
    /// [`ModelError::Tokenizer`], [`ModelError::Ort`] or [`ModelError::Output`].
    pub fn embed_query(&mut self, q: &str) -> Result<[f32; DIM], ModelError> {
        self.embed(&format!("query: {q}"))
    }

    /// Token ids of the NFC form of `text`, without special tokens.
    fn tokenize(&self, text: &str) -> Result<Vec<u32>, ModelError> {
        let text: String = text.nfc().collect();
        // The error names the piece of text it failed on: dropped.
        self.tokenizer
            .encode(&text, true)
            .map_err(|_| ModelError::Tokenizer)
    }

    /// Mean-pooled, L2-normalized embedding of `text`, truncated to [`MAX_TOKENS`] tokens.
    fn embed(&mut self, text: &str) -> Result<[f32; DIM], ModelError> {
        let ids = self.tokenize(text)?;
        let content = ids.len().min(MAX_TOKENS - 2);
        let mut input = Vec::with_capacity(content + 2);
        input.push(BOS);
        input.extend(ids[..content].iter().map(|&id| i64::from(id)));
        input.push(EOS);
        let rows = input.len();
        let mask = vec![1_i64; rows];
        let types = vec![0_i64; rows];
        let shape = [1, rows];

        let mut inputs = ort::inputs![
            INPUT_IDS => TensorRef::from_array_view((shape, input.as_slice()))?,
            ATTENTION_MASK => TensorRef::from_array_view((shape, mask.as_slice()))?,
        ];
        if self.token_types {
            inputs.push((
                TOKEN_TYPE_IDS.into(),
                TensorRef::from_array_view((shape, types.as_slice()))?.into(),
            ));
        }
        let outputs = self.session.run(inputs)?;
        let hidden = outputs.get(HIDDEN_STATE).ok_or(ModelError::Output)?;
        let (dims, data) = hidden.try_extract_tensor::<f32>()?;
        let dims_match = dims
            .iter()
            .map(|&d| usize::try_from(d).ok())
            .eq([1, rows, DIM].map(Some));
        if !dims_match {
            return Err(ModelError::Output);
        }
        mean_pool(data, rows).ok_or(ModelError::Output)
    }
}

/// Mean of the `rows` hidden states of [`DIM`] values (all tokens are unmasked), L2-normalized;
/// `None` for a wrong length or a degenerate result.
fn mean_pool(hidden: &[f32], rows: usize) -> Option<[f32; DIM]> {
    if rows == 0 || hidden.len() != rows.checked_mul(DIM)? {
        return None;
    }
    let mut sum = [0.0_f64; DIM];
    let (rows_of_hidden, _) = hidden.as_chunks::<DIM>();
    for row in rows_of_hidden {
        for (acc, &x) in sum.iter_mut().zip(row) {
            *acc += f64::from(x);
        }
    }
    let rows = f64::from(u32::try_from(rows).ok()?);
    let norm = sum.iter().map(|x| (x / rows).powi(2)).sum::<f64>().sqrt();
    if !norm.is_finite() || norm == 0.0 {
        return None;
    }
    let mut out = [0.0_f32; DIM];
    for (o, s) in out.iter_mut().zip(sum) {
        #[allow(clippy::cast_possible_truncation)] // a unit-vector component fits f32
        let value = (s / rows / norm) as f32;
        *o = value;
    }
    Some(out)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::embed::Manifest;

    #[test]
    fn model_new_needs_loaded_runtime() {
        let files = ModelFiles {
            onnx: Vec::new(),
            kit: Vec::new(),
            manifest: Manifest {
                onnx_sha256: [0; 32],
                kit_sha256: [0; 32],
            },
        };

        let err = Model::new(files, 1).err();

        assert!(matches!(err, Some(ModelError::NotLoaded)), "{err:?}");
    }

    #[test]
    fn telemetry_must_be_disabled_by_exactly_one() {
        use std::ffi::OsStr;
        assert!(telemetry_disabled(Some(OsStr::new("1"))));
        for value in [None, Some("0"), Some(""), Some("true"), Some("1 ")] {
            assert!(!telemetry_disabled(value.map(OsStr::new)), "{value:?}");
        }
    }

    #[test]
    fn ort_log_keeps_errors_without_text() {
        for level in [LogLevel::Verbose, LogLevel::Info, LogLevel::Warning] {
            assert_eq!(log_line(level, "session.cc:12"), None, "{level:?}");
        }
        assert_eq!(
            log_line(LogLevel::Error, "session.cc:12").as_deref(),
            Some("onnxruntime error at session.cc:12")
        );
        assert_eq!(
            log_line(LogLevel::Fatal, "env.cc:3").as_deref(),
            Some("onnxruntime fatal at env.cc:3")
        );
    }

    #[test]
    fn mean_pool_averages_rows_and_normalizes() {
        let mut hidden = vec![0.0; 2 * DIM];
        hidden[0] = 3.0;
        hidden[DIM] = 1.0;
        hidden[1] = 2.0;
        hidden[DIM + 1] = 4.0;

        let v = mean_pool(&hidden, 2).expect("a vector");

        // Mean (2, 3, 0, ...) normalized by sqrt(13).
        let norm = 13.0_f32.sqrt();
        assert!((v[0] - 2.0 / norm).abs() < 1e-6);
        assert!((v[1] - 3.0 / norm).abs() < 1e-6);
        assert!(v[2..].iter().all(|&x| x == 0.0));
    }

    #[test]
    fn mean_pool_rejects_bad_input() {
        assert_eq!(mean_pool(&[0.0; DIM], 2), None, "wrong length");
        assert_eq!(mean_pool(&[], 0), None, "no rows");
        assert_eq!(mean_pool(&[0.0; DIM], 1), None, "zero vector");
        let mut nan = [1.0; DIM];
        nan[7] = f32::NAN;
        assert_eq!(mean_pool(&nan, 1), None, "not finite");
    }
}
