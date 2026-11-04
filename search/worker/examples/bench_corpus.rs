//! Writes the notes of the benchmark set (spec §12.4, `search/testdata/bench`) as JSON for the
//! Go benchmark `BenchmarkSearchService`:
//!
//! `cargo run -p worker --example bench_corpus -- OUT.json`
//!
//! The output is `{"notes": [{"id", "name", "info"}…]}`; the queries stay in `queries.json`.

#[path = "../../testdata/bench/gen.rs"]
#[allow(dead_code, reason = "the benchmark uses other parts")]
mod corpus;

use std::env;
use std::fs;
use std::path::Path;
use std::process::ExitCode;

use serde_json::json;

fn main() -> ExitCode {
    let Some(out) = env::args_os().nth(1) else {
        eprintln!("usage: bench_corpus OUT.json");
        return ExitCode::from(2);
    };
    let queries = Path::new(env!("CARGO_MANIFEST_DIR")).join("../testdata/bench/queries.json");
    let json = match fs::read_to_string(&queries) {
        Ok(json) => json,
        Err(err) => {
            eprintln!("reading {}: {err}", queries.display());
            return ExitCode::FAILURE;
        }
    };
    let notes = corpus::corpus(&corpus::parse_set(&json));
    let notes: Vec<_> = notes
        .iter()
        .map(|n| json!({"id": n.id, "name": n.name, "info": n.info}))
        .collect();
    let body = json!({ "notes": notes }).to_string();
    if let Err(err) = fs::write(&out, body) {
        eprintln!("writing {}: {err}", Path::new(&out).display());
        return ExitCode::FAILURE;
    }
    ExitCode::SUCCESS
}
