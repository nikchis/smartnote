//! The worker's model against reference embeddings of the original fp32 model (spec §12.1):
//! `search/testdata/embed/ref.json` from `tools/model/reference.py` for the texts of
//! `search/testdata/embed/texts.json`.
//!
//! Needs the prepared model (`task model:fetch`) and the ONNX Runtime library
//! (`task ort:fetch`):
//! `SMARTNOTE_MODEL_DIR=$PWD/build/model SMARTNOTE_ORT_LIB=$PWD/build/ort/libonnxruntime.so.1.31.0`.
//!
//! The test loads ONNX Runtime into this process, without the worker's sandbox: `load_ort`
//! needs `ORT_DISABLE_TELEMETRY=1`, which `main` sets before any thread exists. So the binary
//! has its own harness (`harness = false`): libtest would start its threads first. Like a
//! libtest `#[ignore]` test, it runs only with `--ignored` or `--include-ignored`.
#![allow(
    unsafe_code,
    reason = "std::env::set_var while the process is single-threaded"
)]

use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::ExitCode;
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::Instant;

use serde::Deserialize;
use worker::embed::{
    ChunkParams, ChunkSpan, DIM, Manifest, Model, ModelFiles, OrtLibrary, chunk_note, has_avx2,
    load_ort,
};

const MIN_COSINE: f32 = 0.9999;
const THREADS: u16 = 4;

#[derive(Deserialize)]
#[serde(tag = "kind", rename_all = "lowercase")]
enum Text {
    Passage { name: String, body: String },
    Query { text: String },
}

#[derive(Deserialize)]
struct References {
    references: Vec<Reference>,
}

#[derive(Deserialize)]
struct Reference {
    tokens: usize,
    vec: Vec<f32>,
}

fn testdata(name: &str) -> String {
    let path = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../testdata/embed")
        .join(name);
    fs::read_to_string(&path).unwrap_or_else(|err| panic!("reading {}: {err}", path.display()))
}

fn required_path(var: &str) -> PathBuf {
    let value = env::var_os(var);
    PathBuf::from(value.unwrap_or_else(|| panic!("{var} must be set: see tests/embed.rs")))
}

fn cosine(a: &[f32], b: &[f32]) -> f32 {
    let dot: f32 = a.iter().zip(b).map(|(x, y)| x * y).sum();
    let norm = |v: &[f32]| v.iter().map(|x| x * x).sum::<f32>().sqrt();
    dot / (norm(a) * norm(b))
}

/// Resident set size of the process in KiB, on Linux.
fn rss_kib() -> Option<u64> {
    let status = fs::read_to_string("/proc/self/status").ok()?;
    let line = status.lines().find(|l| l.starts_with("VmRSS:"))?;
    line.split_whitespace().nth(1)?.parse().ok()
}

const TEST: &str = "model_embeddings_match_reference";

/// Measurements are printed only with `--nocapture` or `--show-output`, as libtest would.
static SHOW_OUTPUT: AtomicBool = AtomicBool::new(false);

macro_rules! show {
    ($($arg:tt)*) => {
        if SHOW_OUTPUT.load(Ordering::Relaxed) {
            println!($($arg)*);
        }
    };
}
const IGNORE: &str =
    "needs SMARTNOTE_MODEL_DIR and SMARTNOTE_ORT_LIB: task model:fetch, task ort:fetch";

fn main() -> ExitCode {
    // SAFETY: the first statement of `main` of a binary without the libtest harness: no other
    // thread exists (the constructors that run before `main` start none), so none can read or
    // write the environment concurrently.
    unsafe { env::set_var(worker::embed::DISABLE_TELEMETRY, "1") };

    let args: Vec<String> = env::args().skip(1).collect();
    let filters: Vec<&String> = args.iter().filter(|a| !a.starts_with('-')).collect();
    if args.iter().any(|a| a == "--list") {
        println!("{TEST}: test");
        return ExitCode::SUCCESS;
    }
    let selected = filters.is_empty() || filters.iter().any(|f| TEST.contains(f.as_str()));
    let run_ignored = args
        .iter()
        .any(|a| a == "--ignored" || a == "--include-ignored");
    println!("\nrunning {} test", u8::from(selected));
    if !selected {
        println!("\ntest result: ok. 0 passed; 0 failed; 0 ignored; 1 filtered out\n");
    } else if run_ignored {
        let shown = args
            .iter()
            .any(|a| a == "--nocapture" || a == "--show-output");
        SHOW_OUTPUT.store(shown, Ordering::Relaxed);
        // A failed assertion panics, and `panic = "abort"` ends the process with a failure.
        model_embeddings_match_reference();
        println!("test {TEST} ... ok\n\ntest result: ok. 1 passed; 0 failed; 0 ignored\n");
    } else {
        println!(
            "test {TEST} ... ignored, {IGNORE}\n\ntest result: ok. 0 passed; 0 failed; 1 ignored\n"
        );
    }
    ExitCode::SUCCESS
}

/// Chunks a note with the model's own token counts and checks that every model input, the
/// prefix with the part of the name kept and the special tokens included, fits `p.max`.
fn assert_chunks_fit(model: &Model, name: &str, lines: &[&str], p: &ChunkParams) -> Vec<ChunkSpan> {
    let chunks = chunk_note(name, lines, &|t| model.count_tokens(t), p);
    let kept = &name[..chunks.name_len];
    for span in &chunks.spans {
        let first = span.first as usize - 1;
        let body = match span.window {
            Some((start, end)) => lines[first][start..end].to_owned(),
            None => lines[first..span.last as usize].join("\n"),
        };
        let tokens = model.count_tokens(&format!("passage: {kept}\n{body}")) + 2;
        assert!(tokens <= p.max, "{span:?}: {tokens} tokens");
    }
    chunks.spans
}

fn model_embeddings_match_reference() {
    let model_dir = required_path("SMARTNOTE_MODEL_DIR");
    let ort_lib = required_path("SMARTNOTE_ORT_LIB");
    let texts: Vec<Text> = serde_json::from_str(&testdata("texts.json")).expect("texts.json");
    let refs: References = serde_json::from_str(&testdata("ref.json")).expect("ref.json");
    assert_eq!(texts.len(), refs.references.len(), "one reference per text");
    assert!(has_avx2(), "the ONNX Runtime build needs AVX2 on x86_64");

    let files = ModelFiles::read(&model_dir).expect("model files match model.lock.json");
    assert_eq!(
        files.manifest,
        Manifest::pinned(),
        "the manifest of the pinned files"
    );
    let lib = OrtLibrary::verify(&ort_lib).expect("library matches ort.lock.json");
    load_ort(&lib).expect("ONNX Runtime loads");
    load_ort(&lib).expect("loading twice is a no-op");
    let started = Instant::now();
    let mut model = Model::new(files, THREADS).expect("model loads");
    show!(
        "model load {:?}, RSS {:?} KiB",
        started.elapsed(),
        rss_kib()
    );

    let mut worst = f32::INFINITY;
    for (i, (text, reference)) in texts.iter().zip(&refs.references).enumerate() {
        let (model_text, vec) = match text {
            Text::Passage { name, body } => (
                format!("passage: {name}\n{body}"),
                model.embed_passage(name, body),
            ),
            Text::Query { text } => (format!("query: {text}"), model.embed_query(text)),
        };
        let vec = vec.unwrap_or_else(|err| panic!("text {i}: {err}"));
        assert_eq!(reference.vec.len(), DIM, "text {i}");
        assert_eq!(
            model.count_tokens(&model_text),
            reference.tokens,
            "text {i}"
        );
        let similarity = cosine(&vec, &reference.vec);
        assert!(
            similarity >= MIN_COSINE,
            "text {i}: cosine {similarity} < {MIN_COSINE}"
        );
        worst = worst.min(similarity);
    }
    show!("{} texts, min cosine {worst:.7}", texts.len());

    // Chunks of a long note with the model's own token counts stay within the model input.
    let mut note: Vec<String> = texts
        .iter()
        .filter_map(|t| match t {
            Text::Passage { body, .. } => Some(body.clone()),
            Text::Query { .. } => None,
        })
        .collect();
    note.push("слово, word; ".repeat(400));
    let note = note.join("\n\n");
    let lines: Vec<&str> = note.split('\n').collect();
    let p = ChunkParams::default();
    let spans = assert_chunks_fit(&model, "Long note", &lines, &p);
    assert!(
        spans.iter().any(|s| s.window.is_some()),
        "the long line is cut"
    );
    show!("{} chunks of a long note", spans.len());

    // From the review of PR 6: a valid name of 64 U+FDFA expands to some 390 tokens; its chunks
    // still fit the model input, so the distinct tail of the text reaches the model.
    let name = "\u{FDFA}".repeat(64);
    let line = format!("{} cat", "я".repeat(124));
    let spans = assert_chunks_fit(&model, &name, &[line.as_str()], &p);
    let last = spans.last().expect("a chunk");
    assert!(
        last.window.is_none_or(|w| w.1 == line.len()),
        "the tail is covered: {last:?}"
    );
    show!("{} chunks under an expanding name", spans.len());

    // Timing of one chunk of about 256 tokens with the prefix and special tokens.
    let body: String = (1..=16)
        .map(|i| format!("Line {i}: the quick brown fox jumps over the lazy dog."))
        .collect::<Vec<_>>()
        .join("\n");
    let tokens = model.count_tokens(&format!("passage: Timing\n{body}")) + 2;
    let runs = 20;
    let started = Instant::now();
    for _ in 0..runs {
        model.embed_passage("Timing", &body).expect("embeds");
    }
    show!(
        "embed {tokens} tokens: {:?} per chunk ({THREADS} threads), RSS {:?} KiB",
        started.elapsed() / runs,
        rss_kib()
    );
}
