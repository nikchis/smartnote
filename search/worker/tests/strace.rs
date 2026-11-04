//! The worker under `strace -f` on a reference set of notes and queries (spec §10.3, §12.5):
//! no network system calls and no file opened for writing.
//!
//! With `SMARTNOTE_MODEL_DIR` and `SMARTNOTE_ORT_LIB` set (absolute paths) the run includes the
//! model: ONNX Runtime loads and embeds every note under the trace.
#![cfg(target_os = "linux")]

mod common;

use std::fs;
use std::process::Command;

use common::{TempDir, WORKER, Worker, model_paths, upsert_marked};
use worker::protocol::{Reply, Request, State};

const NOTES: u32 = 200;
const QUERIES: u64 = 20;

/// Flags of `open`/`openat` that allow writing or creating a file.
const WRITE_FLAGS: [&str; 4] = ["O_WRONLY", "O_RDWR", "O_CREAT", "O_TRUNC"];

#[test]
#[cfg_attr(strace_missing, ignore = "strace not found")]
fn no_network_no_writes() {
    let dir = TempDir::new("strace");
    let log = dir.path().join("trace.log");
    let mut cmd = Command::new("strace");
    cmd.args(["-f", "-qq", "-e", "trace=%network,openat,open,creat", "-o"])
        .arg(&log)
        .arg("--")
        .arg(WORKER);
    let mut w = Worker::spawn(cmd, true);
    let model = model_paths();
    let with_model = model.is_some();
    match w.hello(model) {
        Reply::Ready { semantic, .. } => assert_eq!(
            semantic != State::Unavailable,
            with_model,
            "semantic state with the model: {with_model}"
        ),
        _ => panic!("expected ready"),
    }

    let first = upsert_marked(&mut w, NOTES, "trace", 1);
    if with_model {
        let mut embedded = 0;
        while embedded < NOTES {
            if let Reply::Embedded { .. } = w.recv() {
                embedded += 1;
            }
        }
    }
    for (req, i) in (first..).zip(0..QUERIES) {
        w.send(&Request::Search {
            req,
            query: format!("trace note {i} пароли"),
            limit: 20,
        });
        w.recv_until(|r| matches!(r, Reply::Result { req: got, .. } if got == req).then_some(()));
    }
    let (status, _) = w.finish();
    assert_eq!(status.code(), Some(0));

    let trace = fs::read_to_string(&log).expect("strace wrote its log");
    let opens = trace
        .lines()
        .filter(|l| l.contains("openat(") || l.contains("open("))
        .count();
    assert!(opens > 0, "the trace has the opens of the loader:\n{trace}");
    for call in ["socket(", "connect(", "sendto(", "creat("] {
        let found: Vec<&str> = trace.lines().filter(|l| l.contains(call)).collect();
        assert!(found.is_empty(), "{call}: {found:#?}");
    }
    let writes: Vec<&str> = trace
        .lines()
        .filter(|l| l.contains("openat(") || l.contains("open("))
        .filter(|l| WRITE_FLAGS.iter().any(|flag| l.contains(flag)))
        .collect();
    assert!(writes.is_empty(), "opened for writing: {writes:#?}");
    println!(
        "{} traced lines, {opens} opens, model: {with_model}",
        trace.lines().count()
    );
}
