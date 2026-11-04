//! Freed memory holds no note text: a core dump of a process that built and freed strings
//! and a tantivy index contains no copy of the marker (spec §6.9, §12.5).
//!
//! The probe runs under `gdb` (its parent, so `ptrace_scope=1` allows it), aborts after
//! freeing everything, and `gdb` writes its core file. Ignored when `gdb` is not found at
//! build time (see `build.rs`).
#![cfg(all(feature = "test-probes", target_os = "linux"))]

use std::fs;
use std::path::PathBuf;
use std::process::Command;

const MARKER: &str = "smartnote-МЕТКА-zalloc-7f3a";

/// Dumps the probe after it freed its data and counts copies of the reversed marker,
/// the form the probe keeps in memory.
fn marker_copies_in_core(zeroing: bool) -> usize {
    let mode = if zeroing { "on" } else { "off" };
    let core = PathBuf::from(env!("CARGO_TARGET_TMPDIR"))
        .join(format!("probe_zalloc-{}-{mode}.core", std::process::id()));
    let generate = format!("generate-core-file {}", core.display());
    let mut gdb = Command::new("gdb");
    gdb.args(["-nx", "-batch", "-iex", "set debuginfod enabled off"])
        .args(["-ex", "set startup-with-shell off", "-ex", "run"])
        .args(["-ex", &generate, "--args"])
        .args([env!("CARGO_BIN_EXE_probe_zalloc"), "dump", MARKER]);
    if !zeroing {
        gdb.arg("off");
    }
    let out = gdb.output().expect("gdb starts");
    let stdout = String::from_utf8_lossy(&out.stdout);
    assert!(
        stdout.contains("SIGABRT"),
        "the probe must stop at its abort; gdb stdout: {stdout}\nstderr: {}",
        String::from_utf8_lossy(&out.stderr)
    );
    let dump = fs::read(&core).expect("gdb wrote the core file");
    fs::remove_file(&core).expect("remove the core file");

    let needle: String = MARKER.chars().rev().collect();
    let needle = needle.as_bytes();
    dump.windows(needle.len()).filter(|w| *w == needle).count()
}

#[test]
#[cfg_attr(gdb_missing, ignore = "gdb not found")]
fn freed_marker_not_in_core() {
    let control = marker_copies_in_core(false);
    assert!(
        control > 0,
        "control run without zeroing must keep the marker"
    );
    assert_eq!(marker_copies_in_core(true), 0);
}
