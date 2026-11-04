//! Quality of the search on the benchmark set (spec §12.4): the share of queries whose note is
//! among the first 5 hits, the share of irrelevant notes among purely semantic candidates and
//! the share of queries that must find nothing and do, for the hybrid search and for each kind
//! of search alone, by chunk size and semantic threshold. The result is a report: it never
//! fails on quality.
//!
//! The set is `search/testdata/bench`: `gen.rs` makes the notes from a fixed seed and
//! `queries.json` gives the planted paragraphs and the queries.
//!
//! Two tests, with their own harness like `tests/embed.rs` (ONNX Runtime needs
//! `ORT_DISABLE_TELEMETRY=1` before any thread starts):
//!
//! - `bench_set_shape` checks the generator: kinds, lengths, plants, determinism;
//! - `bench_quality` (ignored: needs `SMARTNOTE_MODEL_DIR` and `SMARTNOTE_ORT_LIB`) measures the
//!   worker's chunking and threshold. With `SMARTNOTE_BENCH=sweep` it measures the chunk sizes
//!   192/256/320 and the thresholds 0.78–0.90; tables go to stdout with `--nocapture`.
//!   `SMARTNOTE_BENCH_MODELS` sets the number of models embedding in parallel (default 8, each
//!   with 4 threads). Use a release build: `cargo test --release -p worker --test bench --
//!   --ignored --nocapture`.
#![allow(
    unsafe_code,
    reason = "std::env::set_var while the process is single-threaded"
)]
#![allow(
    clippy::cast_precision_loss,
    reason = "counts of queries and notes are far below 2^52"
)]

#[path = "../../testdata/bench/gen.rs"]
#[allow(dead_code, reason = "the example uses other parts")]
mod corpus;

use std::collections::{BTreeMap, HashMap};
use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::ExitCode;
use std::sync::Mutex;
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::thread;
use std::time::{Duration, Instant};

use serde_bytes::ByteBuf;
use worker::embed::{
    ChunkParams, ChunkVec, DIM, Manifest, Model, ModelFiles, OrtLibrary, Record, chunk_note,
    has_avx2, index_id, load_ort, text_hash,
};
use worker::engine::{Engine, Upserted};
use worker::protocol::messages::{Hit, NoteIn, Source};
use worker::rank::TAU;
use worker::text::{Query, parse_query};

use corpus::{BenchQuery, Kind, MAX_UTF16, MIN_UTF16, NOTES, Note, Set, corpus, parse_set};

/// Hits a query shows: the limit of the application.
const LIMIT: usize = 10;
/// The first hits that count for recall.
const TOP: usize = 5;
/// Chunk targets of the sweep.
const TARGETS: [usize; 3] = [192, 256, 320];
/// Thresholds of the sweep: the spec's 0.78–0.84 and above.
const TAUS: [f32; 7] = [0.78, 0.80, 0.82, 0.84, 0.86, 0.88, 0.90];
/// Intra-op threads of each model.
const MODEL_THREADS: u16 = 4;

static SHOW_OUTPUT: AtomicBool = AtomicBool::new(false);

macro_rules! show {
    ($($arg:tt)*) => {
        if SHOW_OUTPUT.load(Ordering::Relaxed) {
            println!($($arg)*);
        }
    };
}

const SHAPE: &str = "bench_set_shape";
const QUALITY: &str = "bench_quality";
const IGNORE: &str =
    "needs SMARTNOTE_MODEL_DIR and SMARTNOTE_ORT_LIB: task model:fetch, task ort:fetch";

fn main() -> ExitCode {
    // SAFETY: the first statement of `main` of a binary without the libtest harness: no other
    // thread exists, so none can read or write the environment concurrently.
    unsafe { env::set_var(worker::embed::DISABLE_TELEMETRY, "1") };

    let args: Vec<String> = env::args().skip(1).collect();
    if args.iter().any(|a| a == "--list") {
        println!("{SHAPE}: test\n{QUALITY}: test");
        return ExitCode::SUCCESS;
    }
    let filters: Vec<&String> = args.iter().filter(|a| !a.starts_with('-')).collect();
    let selected =
        |name: &str| filters.is_empty() || filters.iter().any(|f| name.contains(f.as_str()));
    let ignored_only = args.iter().any(|a| a == "--ignored");
    let include_ignored = ignored_only || args.iter().any(|a| a == "--include-ignored");
    let shown = args
        .iter()
        .any(|a| a == "--nocapture" || a == "--show-output");
    SHOW_OUTPUT.store(shown, Ordering::Relaxed);

    let run_shape = selected(SHAPE) && !ignored_only;
    let run_quality = selected(QUALITY) && include_ignored;
    let ignored = usize::from(selected(QUALITY) && !include_ignored);
    let count = usize::from(run_shape) + usize::from(run_quality);
    println!("\nrunning {count} tests");
    // A failed assertion panics, and `panic = "abort"` ends the process with a failure.
    if run_shape {
        bench_set_shape();
        println!("test {SHAPE} ... ok");
    }
    if run_quality {
        bench_quality();
        println!("test {QUALITY} ... ok");
    }
    if ignored == 1 {
        println!("test {QUALITY} ... ignored, {IGNORE}");
    }
    println!("\ntest result: ok. {count} passed; 0 failed; {ignored} ignored\n");
    ExitCode::SUCCESS
}

fn load_set() -> Set {
    let path = Path::new(env!("CARGO_MANIFEST_DIR")).join("../testdata/bench/queries.json");
    let json =
        fs::read_to_string(&path).unwrap_or_else(|err| panic!("reading {}: {err}", path.display()));
    parse_set(&json)
}

fn bench_set_shape() {
    let set = load_set();
    let notes = corpus(&set);
    assert_eq!(notes.len(), NOTES);
    let count = |kind| notes.iter().filter(|n| n.kind == kind).count();
    assert_eq!(
        [
            count(Kind::Ru),
            count(Kind::En),
            count(Kind::Code),
            count(Kind::Mixed)
        ],
        [400, 300, 200, 100]
    );
    for n in &notes {
        let len = n.info.encode_utf16().count();
        let planted = set.plants.iter().any(|p| n.id == format!("n{:04}", p.note));
        assert!(
            (planted || len >= MIN_UTF16 - 2) && len <= MAX_UTF16,
            "{}: {len} UTF-16 units",
            n.id
        );
    }
    for p in &set.plants {
        let info = &notes[p.note].info;
        assert!(
            info.contains(&p.lines.join("\n")),
            "plant of note {}",
            p.note
        );
    }
    for q in &set.queries {
        if let Some(note) = q.expect {
            assert!(
                set.plants.iter().any(|p| p.note == note),
                "{}: no plant",
                q.q
            );
        }
    }
    let again = corpus(&set);
    assert!(
        notes
            .iter()
            .zip(&again)
            .all(|(a, b)| a.name == b.name && a.info == b.info),
        "the generator is deterministic"
    );
    let total: usize = notes.iter().map(|n| n.info.encode_utf16().count()).sum();
    show!(
        "{} notes, {} plants, {} queries, {total} UTF-16 units of text",
        notes.len(),
        set.plants.len(),
        set.queries.len()
    );
}

fn required_path(var: &str) -> PathBuf {
    PathBuf::from(
        env::var_os(var).unwrap_or_else(|| panic!("{var} must be set: see tests/bench.rs")),
    )
}

/// The kinds of search a run uses.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Mode {
    Hybrid,
    Words,
    Substring,
    Semantic,
}

const MODES: [Mode; 4] = [Mode::Hybrid, Mode::Words, Mode::Substring, Mode::Semantic];

impl Mode {
    fn name(self) -> &'static str {
        match self {
            Mode::Hybrid => "hybrid",
            Mode::Words => "words",
            Mode::Substring => "substring",
            Mode::Semantic => "semantic",
        }
    }

    /// `q` restricted to the mode, and whether the query vector goes with it.
    fn restrict(self, q: &Query) -> (Query, bool) {
        match self {
            Mode::Hybrid => (q.clone(), true),
            Mode::Words => (
                Query {
                    substring: None,
                    semantic: None,
                    ..q.clone()
                },
                false,
            ),
            Mode::Substring => (
                Query {
                    substring: q.substring.clone(),
                    ..Query::default()
                },
                false,
            ),
            Mode::Semantic => (
                Query {
                    semantic: q.semantic.clone(),
                    ..Query::default()
                },
                true,
            ),
        }
    }
}

/// A query of the set with its parsed form and vector.
struct Prepared<'a> {
    q: &'a BenchQuery,
    parsed: Query,
    vec: Option<[f32; DIM]>,
}

fn bench_quality() {
    let model_dir = required_path("SMARTNOTE_MODEL_DIR");
    let ort_lib = required_path("SMARTNOTE_ORT_LIB");
    assert!(has_avx2(), "the ONNX Runtime build needs AVX2 on x86_64");
    let lib = OrtLibrary::verify(&ort_lib).expect("library matches ort.lock.json");
    load_ort(&lib).expect("ONNX Runtime loads");
    let sweep = env::var("SMARTNOTE_BENCH").is_ok_and(|v| v == "sweep");
    let models: usize = env::var("SMARTNOTE_BENCH_MODELS")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(8);

    let set = load_set();
    let notes = corpus(&set);
    let mut model = Model::new(
        ModelFiles::read(&model_dir).expect("model files"),
        MODEL_THREADS,
    )
    .expect("model loads");
    let queries: Vec<Prepared> = set
        .queries
        .iter()
        .map(|q| {
            let parsed = parse_query(&q.q);
            let vec = parsed
                .semantic
                .as_deref()
                .map(|text| model.embed_query(text).expect("query embeds"));
            Prepared { q, parsed, vec }
        })
        .collect();
    report_plants_alone(&mut model, &set, &queries);
    drop(model);

    let defaults = ChunkParams::default();
    let (targets, taus): (Vec<usize>, Vec<f32>) = if sweep {
        (TARGETS.to_vec(), TAUS.to_vec())
    } else {
        (vec![defaults.target], vec![TAU])
    };
    show!(
        "\n# Benchmark set: {} notes, {} queries; current target {}, τ {TAU}",
        notes.len(),
        queries.len(),
        defaults.target
    );
    for target in targets {
        let params = ChunkParams { target, ..defaults };
        let started = Instant::now();
        let chunks = embed_notes(&notes, &params, &model_dir, models);
        let total: usize = chunks.iter().map(Vec::len).sum();
        show!(
            "\n## Chunk target {target}: {total} chunks, {:.1} per note, embedded in {:.0?} ({models} models × {MODEL_THREADS} threads)",
            total as f64 / notes.len() as f64,
            started.elapsed()
        );
        let mut engine = build_engine(&notes, &chunks, &params);
        report_target(&mut engine, &notes, &chunks, &queries, &taus);
    }
}

/// Embeddings of every note with `params`, by `models` models in parallel.
fn embed_notes(
    notes: &[Note],
    params: &ChunkParams,
    dir: &Path,
    models: usize,
) -> Vec<Vec<ChunkVec>> {
    let next = AtomicUsize::new(0);
    let out: Mutex<Vec<Option<Vec<ChunkVec>>>> = Mutex::new(vec![None; notes.len()]);
    thread::scope(|s| {
        for _ in 0..models.max(1) {
            s.spawn(|| {
                let files = ModelFiles::read(dir).expect("model files");
                let mut model = Model::new(files, MODEL_THREADS).expect("model loads");
                loop {
                    let i = next.fetch_add(1, Ordering::Relaxed);
                    let Some(note) = notes.get(i) else { break };
                    let chunks = embed_note(&mut model, note, params);
                    out.lock().expect("lock")[i] = Some(chunks);
                }
            });
        }
    });
    out.into_inner()
        .expect("lock")
        .into_iter()
        .map(|c| c.expect("every note embedded"))
        .collect()
}

/// Chunks and embeds a note as the worker's embed thread does.
fn embed_note(model: &mut Model, note: &Note, params: &ChunkParams) -> Vec<ChunkVec> {
    let lines: Vec<&str> = note.info.split('\n').collect();
    let spans = chunk_note(&note.name, &lines, &|t| model.count_tokens(t), params);
    spans
        .iter()
        .map(|span| {
            let first = span.first as usize - 1;
            let body = match span.window {
                Some((start, end)) => lines[first][start..end].to_owned(),
                None => lines[first..span.last as usize].join("\n"),
            };
            let vec = model
                .embed_passage(&note.name, &body)
                .expect("chunk embeds");
            ChunkVec {
                first: span.first,
                last: span.last,
                vec,
            }
        })
        .collect()
}

fn build_engine(notes: &[Note], chunks: &[Vec<ChunkVec>], params: &ChunkParams) -> Engine {
    let index = index_id(&Manifest::pinned(), params);
    let mut engine = Engine::new(index);
    for (i, (note, chunks)) in notes.iter().zip(chunks).enumerate() {
        let record = Record {
            index,
            text: text_hash(&note.name, &note.info),
            chunks: chunks.clone(),
        };
        let upserted = engine.upsert(NoteIn {
            id: note.id.clone(),
            rev: i as u64 + 1,
            name: note.name.clone(),
            info: note.info.clone(),
            emb: Some(ByteBuf::from(record.encode())),
        });
        assert_eq!(upserted, Some(Upserted::Fresh), "{}", note.id);
    }
    engine.commit();
    engine
}

fn note_index(id: &str) -> usize {
    id[1..].parse().expect("note id")
}

/// Measures of one run over every query.
#[derive(Default)]
struct Measure {
    positives: usize,
    found_top: usize,
    found_first: usize,
    /// Hits with the semantic source alone, and those of other notes than the expected one.
    semantic_only: usize,
    semantic_only_irrelevant: usize,
    nonsense: usize,
    nonsense_empty: usize,
    absent: usize,
    absent_empty: usize,
    /// Recall by kind of query: (found in top, total).
    by_kind: BTreeMap<String, (usize, usize)>,
    /// Queries whose note is not in the top.
    missed: Vec<String>,
    times: Vec<Duration>,
}

fn run(engine: &Engine, queries: &[Prepared], mode: Mode) -> Measure {
    let mut m = Measure::default();
    for p in queries {
        let (q, with_vec) = mode.restrict(&p.parsed);
        let vec = if with_vec { p.vec.as_ref() } else { None };
        let started = Instant::now();
        let hits = engine.search(&q, vec, LIMIT);
        m.times.push(started.elapsed());
        score_query(&mut m, p.q, &hits);
    }
    m
}

fn score_query(m: &mut Measure, q: &BenchQuery, hits: &[Hit]) {
    let expected = q.expect.map(|n| format!("n{n:04}"));
    for h in hits {
        if h.sources == [Source::Semantic] {
            m.semantic_only += 1;
            if expected.as_deref() != Some(h.note.as_str()) {
                m.semantic_only_irrelevant += 1;
            }
        }
    }
    match (q.kind.as_str(), expected) {
        ("nonsense", _) => {
            m.nonsense += 1;
            m.nonsense_empty += usize::from(hits.is_empty());
        }
        ("absent", _) => {
            m.absent += 1;
            m.absent_empty += usize::from(hits.is_empty());
        }
        (kind, Some(id)) => {
            m.positives += 1;
            let top = hits.iter().take(TOP).any(|h| h.note == id);
            m.found_top += usize::from(top);
            m.found_first += usize::from(hits.first().is_some_and(|h| h.note == id));
            let entry = m.by_kind.entry(kind.to_owned()).or_default();
            entry.0 += usize::from(top);
            entry.1 += 1;
            if !top {
                let first = hits.first().map_or("—".to_owned(), |h| {
                    format!("{} {}", h.note, note_index(&h.note))
                });
                m.missed.push(format!("{} ({kind}; first: {first})", q.q));
            }
        }
        (kind, None) => panic!("{}: kind {kind} without a note", q.q),
    }
}

fn pct(a: usize, b: usize) -> String {
    if b == 0 {
        "—".to_owned()
    } else {
        format!("{:.1}%", 100.0 * a as f64 / b as f64)
    }
}

/// `(p50, p95, max)` of `times` in milliseconds.
fn time_stats(times: &[Duration]) -> (f64, f64, f64) {
    let mut ms: Vec<f64> = times.iter().map(|d| d.as_secs_f64() * 1000.0).collect();
    ms.sort_by(f64::total_cmp);
    let at = |q: f64| {
        #[expect(
            clippy::cast_possible_truncation,
            clippy::cast_sign_loss,
            reason = "a rank within the list"
        )]
        let i = ((ms.len() - 1) as f64 * q).round() as usize;
        ms[i]
    };
    (at(0.5), at(0.95), at(1.0))
}

fn report_target(
    engine: &mut Engine,
    notes: &[Note],
    chunks: &[Vec<ChunkVec>],
    queries: &[Prepared],
    taus: &[f32],
) {
    show!(
        "\n| τ | mode | recall@5 | first | semantic-only hits | irrelevant among them | nonsense empty | absent empty | p50 ms | p95 ms |"
    );
    show!("|---|---|---|---|---|---|---|---|---|---|");
    let mut details = Vec::new();
    for (i, &tau) in taus.iter().enumerate() {
        engine.set_semantic_threshold(tau);
        for mode in MODES {
            // Words and substrings do not depend on τ: once.
            if matches!(mode, Mode::Words | Mode::Substring) && i > 0 {
                continue;
            }
            let m = run(engine, queries, mode);
            let (p50, p95, _) = time_stats(&m.times);
            show!(
                "| {} | {} | {} ({}/{}) | {} | {} | {} | {} | {} | {p50:.2} | {p95:.2} |",
                if matches!(mode, Mode::Words | Mode::Substring) {
                    "—".to_owned()
                } else {
                    format!("{tau:.2}")
                },
                mode.name(),
                pct(m.found_top, m.positives),
                m.found_top,
                m.positives,
                pct(m.found_first, m.positives),
                m.semantic_only,
                pct(m.semantic_only_irrelevant, m.semantic_only),
                pct(m.nonsense_empty, m.nonsense),
                pct(m.absent_empty, m.absent),
            );
            if mode == Mode::Hybrid {
                details.push((tau, m));
            }
        }
    }
    for (tau, m) in &details {
        show!("\nHybrid, τ {tau:.2}: recall@5 by kind of query");
        for (kind, (found, total)) in &m.by_kind {
            show!("- {kind}: {}", pct(*found, *total));
        }
        if !m.missed.is_empty() {
            show!("  missed: {}", m.missed.join("; "));
        }
    }
    report_candidates(chunks, queries, taus);
    report_scores(notes, chunks, queries);
    report_substring_time(engine, queries);
}

/// Best score of each note for a query vector.
fn note_scores(chunks: &[Vec<ChunkVec>], qvec: &[f32; DIM]) -> Vec<f32> {
    chunks
        .iter()
        .map(|cs| {
            cs.iter()
                .map(|c| dot(&c.vec, qvec))
                .fold(f32::NEG_INFINITY, f32::max)
        })
        .collect()
}

fn dot(a: &[f32; DIM], b: &[f32; DIM]) -> f32 {
    a.iter().zip(b).map(|(x, y)| x * y).sum()
}

/// The whole semantic branch by τ: notes with a chunk at or above τ (the branch takes up to 50
/// lines, at most 3 a note), the irrelevant share among them, the share of positive queries
/// whose note passes, and of nonsense queries without any candidate.
fn report_candidates(chunks: &[Vec<ChunkVec>], queries: &[Prepared], taus: &[f32]) {
    show!("\nSemantic branch alone, by note (every note with a chunk ≥ τ):");
    show!(
        "\n| τ | candidate notes per query (mean) | irrelevant share | positive queries whose note passes | nonsense/absent without candidates |"
    );
    show!("|---|---|---|---|---|");
    for &tau in taus {
        let (mut cands, mut irrelevant, mut pos, mut pos_pass, mut neg, mut neg_none) =
            (0, 0, 0, 0, 0, 0);
        for p in queries {
            let Some(qvec) = &p.vec else { continue };
            let scores = note_scores(chunks, qvec);
            let passing: Vec<usize> = (0..scores.len()).filter(|&i| scores[i] >= tau).collect();
            cands += passing.len();
            irrelevant += passing.iter().filter(|&&i| Some(i) != p.q.expect).count();
            if let Some(n) = p.q.expect {
                pos += 1;
                pos_pass += usize::from(scores[n] >= tau);
            } else {
                neg += 1;
                neg_none += usize::from(passing.is_empty());
            }
        }
        let with_vec = queries.iter().filter(|p| p.vec.is_some()).count();
        show!(
            "| {tau:.2} | {:.1} | {} | {} ({pos_pass}/{pos}) | {} ({neg_none}/{neg}) |",
            cands as f64 / with_vec.max(1) as f64,
            pct(irrelevant, cands),
            pct(pos_pass, pos),
            pct(neg_none, neg),
        );
    }
}

/// Five-number summary of `v`.
fn summary(v: &mut [f32]) -> String {
    if v.is_empty() {
        return "—".to_owned();
    }
    v.sort_by(f32::total_cmp);
    let at = |q: f64| {
        #[expect(
            clippy::cast_possible_truncation,
            clippy::cast_sign_loss,
            reason = "a rank within the list"
        )]
        let i = ((v.len() - 1) as f64 * q).round() as usize;
        v[i]
    };
    format!(
        "{:.3} / {:.3} / {:.3} / {:.3} / {:.3}",
        at(0.0),
        at(0.1),
        at(0.5),
        at(0.9),
        at(1.0)
    )
}

/// Score distributions for the threshold: the best chunk of the expected note, the best chunk
/// of any other note, the best chunk for queries that must find nothing; and per-query
/// measures an adaptive rule could use: the gap of the best note over the 10th best and the
/// z-score of the best note among all notes.
fn report_scores(notes: &[Note], chunks: &[Vec<ChunkVec>], queries: &[Prepared]) {
    let mut rel = Vec::new();
    let mut rel_rank1 = 0;
    let mut other = Vec::new();
    let mut neg_best = Vec::new();
    let (mut pos_gap, mut neg_gap, mut pos_z, mut neg_z) =
        (Vec::new(), Vec::new(), Vec::new(), Vec::new());
    let mut rows = Vec::new();
    for p in queries {
        let Some(qvec) = &p.vec else { continue };
        let scores = note_scores(chunks, qvec);
        let mut order: Vec<usize> = (0..scores.len()).collect();
        order.sort_by(|&a, &b| scores[b].total_cmp(&scores[a]));
        let best = scores[order[0]];
        let tenth = scores[order[9]];
        let mean = scores.iter().sum::<f32>() / scores.len() as f32;
        let sd =
            (scores.iter().map(|s| (s - mean).powi(2)).sum::<f32>() / scores.len() as f32).sqrt();
        let z = (best - mean) / sd;
        let Some(n) = p.q.expect else {
            neg_best.push(best);
            neg_gap.push(best - tenth);
            neg_z.push(z);
            rows.push(format!(
                "| {} | {} | — | — | {best:.3} ({}) |",
                p.q.q, p.q.kind, notes[order[0]].id
            ));
            continue;
        };
        rel.push(scores[n]);
        let other_note = order.iter().copied().find(|&i| i != n).expect("other");
        let best_other = scores[other_note];
        other.push(best_other);
        let rank = order.iter().position(|&i| i == n).expect("note") + 1;
        rel_rank1 += usize::from(rank == 1);
        if order[0] == n {
            pos_gap.push(best - tenth);
            pos_z.push(z);
        }
        rows.push(format!(
            "| {} | {} | {:.3} | {rank} | {best_other:.3} ({}) |",
            p.q.q.replace('|', "\\|"),
            p.q.kind,
            scores[n],
            notes[other_note].id
        ));
    }
    show!("\nScores (dot product of the best chunk), min / p10 / p50 / p90 / max:");
    show!("- expected note: {}", summary(&mut rel));
    show!(
        "- best other note, positive queries: {}",
        summary(&mut other)
    );
    show!(
        "- best note, nonsense and absent queries: {}",
        summary(&mut neg_best)
    );
    show!(
        "- expected note ranks first semantically: {}",
        pct(rel_rank1, rel.len())
    );
    show!(
        "- gap best − 10th best note, positive queries ranked first: {}",
        summary(&mut pos_gap)
    );
    show!(
        "- gap best − 10th best note, nonsense and absent queries: {}",
        summary(&mut neg_gap)
    );
    show!(
        "- z-score of the best note, positive queries ranked first: {}",
        summary(&mut pos_z)
    );
    show!(
        "- z-score of the best note, nonsense and absent queries: {}",
        summary(&mut neg_z)
    );
    show!("\n| query | kind | expected note score | its rank | best other note |");
    show!("|---|---|---|---|---|");
    for row in rows {
        show!("{row}");
    }
}

/// Scores of each query against its planted paragraph embedded alone as one chunk (title and
/// lines, no filler around it): what the model gives when chunking does not dilute the plant.
fn report_plants_alone(model: &mut Model, set: &Set, queries: &[Prepared]) {
    let plants: HashMap<usize, [f32; DIM]> = set
        .plants
        .iter()
        .map(|p| {
            let vec = model
                .embed_passage(&p.title, &p.lines.join("\n"))
                .expect("plant embeds");
            (p.note, vec)
        })
        .collect();
    let mut scores = Vec::new();
    show!("\nPlanted paragraph alone as a chunk (independent of the chunk target):");
    show!("\n| query | kind | score against the plant alone |");
    show!("|---|---|---|");
    for p in queries {
        let (Some(note), Some(qvec)) = (p.q.expect, &p.vec) else {
            continue;
        };
        let score = dot(&plants[&note], qvec);
        scores.push(score);
        show!(
            "| {} | {} | {score:.3} |",
            p.q.q.replace('|', "\\|"),
            p.q.kind
        );
    }
    show!(
        "- summary min / p10 / p50 / p90 / max: {}",
        summary(&mut scores)
    );
}

/// Time of the substring search alone for the queries that run it, in the engine.
fn report_substring_time(engine: &Engine, queries: &[Prepared]) {
    let mut times: HashMap<&str, Duration> = HashMap::new();
    let rounds = 20;
    for p in queries {
        let Some(needle) = &p.parsed.substring else {
            continue;
        };
        let q = Query {
            substring: Some(needle.clone()),
            ..Query::default()
        };
        let started = Instant::now();
        for _ in 0..rounds {
            std::hint::black_box(engine.search(&q, None, LIMIT));
        }
        times.insert(&p.q.q, started.elapsed() / rounds);
    }
    let all: Vec<Duration> = times.values().copied().collect();
    let (p50, p95, max) = time_stats(&all);
    show!(
        "\nSubstring search alone in the engine ({} queries, mean of {rounds} runs each): p50 {p50:.2} ms, p95 {p95:.2} ms, max {max:.2} ms",
        all.len()
    );
}
