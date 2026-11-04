//! Test helper (feature `test-probes`): the worker's panic hook, allocator and hardening
//! without the worker loop. Used by `tests/{panic_hook,zalloc_dump,harden}.rs`.
//!
//! - `probe_zalloc panic <marker>` — installs the hook and panics on a slice of `<marker>`
//!   off a char boundary.
//! - `probe_zalloc dump <marker> [off]` — builds 2000 strings and a tantivy index holding the
//!   reversed marker, frees them and aborts; `off` turns zeroing off (control run). The
//!   marker is reversed so that the copy in `argv` does not count.
//! - `probe_zalloc sleep [off | <parent pid>]` — hardens the process (with the parent pid
//!   check when given), prints `ready` and sleeps; `off` skips hardening (control run).
#![allow(
    unsafe_code,
    reason = "the switchable global allocator implements GlobalAlloc"
)]

use std::alloc::{GlobalAlloc, Layout, System};
use std::io::{self, Write};
use std::process::ExitCode;
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread;
use std::time::Duration;

use tantivy::collector::TopDocs;
use tantivy::query::QueryParser;
use tantivy::schema::{STORED, Schema, TEXT, Value};
use tantivy::{Index, IndexWriter, ReloadPolicy, TantivyDocument, doc};
use zalloc::Zeroizing;

/// Probe lines in the dump test; the index holds one document per line.
const LINES: usize = 2000;
/// How long `sleep` waits for the test to inspect the process.
const SLEEP: Duration = Duration::from_secs(30);

static ZEROING: AtomicBool = AtomicBool::new(true);

/// [`Zeroizing`] or, after `off`, the plain system allocator. Both hand out `System` blocks,
/// so a block may be freed by either.
struct Switchable;

impl Switchable {
    fn zeroing() -> bool {
        ZEROING.load(Ordering::Relaxed)
    }
}

// SAFETY: every call is forwarded unchanged to `Zeroizing` or `System`; `Zeroizing` forwards
// its blocks to `System`, so a block allocated through one may be released through the other.
unsafe impl GlobalAlloc for Switchable {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        // SAFETY: the caller upholds the `alloc` contract.
        unsafe { System.alloc(layout) }
    }

    unsafe fn alloc_zeroed(&self, layout: Layout) -> *mut u8 {
        // SAFETY: the caller upholds the `alloc_zeroed` contract.
        unsafe { System.alloc_zeroed(layout) }
    }

    unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
        if Self::zeroing() {
            // SAFETY: the caller upholds the `dealloc` contract; the block is a `System` block.
            unsafe { Zeroizing.dealloc(ptr, layout) }
        } else {
            // SAFETY: as above.
            unsafe { System.dealloc(ptr, layout) }
        }
    }

    unsafe fn realloc(&self, ptr: *mut u8, layout: Layout, new_size: usize) -> *mut u8 {
        if Self::zeroing() {
            // SAFETY: the caller upholds the `realloc` contract; the block is a `System` block.
            unsafe { Zeroizing.realloc(ptr, layout, new_size) }
        } else {
            // SAFETY: as above.
            unsafe { System.realloc(ptr, layout, new_size) }
        }
    }
}

#[global_allocator]
static ALLOC: Switchable = Switchable;

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let args: Vec<&str> = args.iter().map(String::as_str).collect();
    match args.as_slice() {
        ["panic", marker] => panic_on(marker),
        ["dump", marker] => dump(marker),
        ["dump", marker, "off"] => {
            ZEROING.store(false, Ordering::Relaxed);
            dump(marker)
        }
        ["sleep"] => sleep(&Hardening::On { parent_pid: None }),
        ["sleep", "off"] => sleep(&Hardening::Off),
        ["sleep", pid] => match pid.parse() {
            Ok(pid) => sleep(&Hardening::On {
                parent_pid: Some(pid),
            }),
            Err(_) => usage(),
        },
        _ => usage(),
    }
}

fn usage() -> ExitCode {
    eprintln!("usage: probe_zalloc panic <marker> | dump <marker> [off] | sleep [off|<pid>]");
    ExitCode::from(2)
}

fn panic_on(marker: &str) -> ExitCode {
    worker::panic::install_hook();
    let text = std::hint::black_box(marker.to_owned());
    let Some(cut) = text
        .char_indices()
        .find(|(_, c)| c.len_utf8() > 1)
        .map(|(i, _)| i + 1)
    else {
        return usage();
    };
    let part = &text[..cut];
    println!("{}", part.len());
    ExitCode::SUCCESS
}

fn dump(marker: &str) -> ExitCode {
    let secret: String = marker.chars().rev().collect();
    let lines: Vec<String> = (0..LINES)
        .map(|i| format!("строка {i} {secret} конец"))
        .collect();
    drop(secret);
    let indexed = index_lines(&lines);
    drop(lines);
    match indexed {
        Ok(found) if found == LINES => std::process::abort(),
        Ok(found) => {
            eprintln!("index returned {found} documents");
            ExitCode::FAILURE
        }
        Err(err) => {
            eprintln!("index failed: {err}");
            ExitCode::FAILURE
        }
    }
}

/// Indexes `lines` in RAM, reads a stored document back and returns the number of hits of a
/// query matching every line. Everything is dropped before returning.
fn index_lines(lines: &[String]) -> tantivy::Result<usize> {
    let mut builder = Schema::builder();
    let body = builder.add_text_field("body", TEXT | STORED);
    let index = Index::create_in_ram(builder.build());
    let mut writer: IndexWriter = index.writer_with_num_threads(1, 15_000_000)?;
    for line in lines {
        writer.add_document(doc!(body => line.as_str()))?;
    }
    writer.commit()?;
    writer.wait_merging_threads()?;

    let reader = index
        .reader_builder()
        .reload_policy(ReloadPolicy::Manual)
        .try_into()?;
    let searcher = reader.searcher();
    let query = QueryParser::for_index(&index, vec![body]).parse_query("конец")?;
    let hits = searcher.search(&query, &TopDocs::with_limit(LINES).order_by_score())?;
    for (_, address) in hits.iter().take(10) {
        let stored: TantivyDocument = searcher.doc(*address)?;
        if stored.get_first(body).and_then(|v| v.as_str()).is_none() {
            return Ok(0);
        }
    }
    Ok(hits.len())
}

enum Hardening {
    Off,
    On { parent_pid: Option<u32> },
}

fn sleep(hardening: &Hardening) -> ExitCode {
    if let Hardening::On { parent_pid } = *hardening
        && let Err(err) = worker::harden::harden_process(parent_pid)
    {
        eprintln!("harden failed: {err}");
        return ExitCode::FAILURE;
    }
    let mut out = io::stdout().lock();
    if writeln!(out, "ready").and_then(|()| out.flush()).is_err() {
        return ExitCode::FAILURE;
    }
    drop(out);
    thread::sleep(SLEEP);
    ExitCode::SUCCESS
}
