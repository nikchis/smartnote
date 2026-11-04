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

/// Copies of the reversed marker in a core dump of the probe, with a report of where they
/// are when there are any.
struct Copies {
    count: usize,
    report: String,
}

/// Dumps the probe after it freed its data and counts copies of the reversed marker,
/// the form the probe keeps in memory.
fn marker_copies_in_core(zeroing: bool) -> Copies {
    let mode = if zeroing { "on" } else { "off" };
    let core = PathBuf::from(env!("CARGO_TARGET_TMPDIR"))
        .join(format!("probe_zalloc-{}-{mode}.core", std::process::id()));
    let generate = format!("generate-core-file {}", core.display());
    let mut gdb = Command::new("gdb");
    gdb.args(["-nx", "-batch", "-iex", "set debuginfod enabled off"])
        .args(["-ex", "set startup-with-shell off", "-ex", "run"])
        .args(["-ex", "info proc mappings", "-ex", &generate, "--args"])
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
    let offsets: Vec<usize> = dump
        .windows(needle.len())
        .enumerate()
        .filter(|(_, w)| *w == needle)
        .map(|(at, _)| at)
        .collect();
    let report = if offsets.is_empty() {
        String::new()
    } else {
        where_in_core(&dump, &offsets, &stdout)
    };
    Copies {
        count: offsets.len(),
        report,
    }
}

/// Where the copies at `offsets` of the core file `dump` are: the segment of the core, the
/// address in the probe and the mapping of `info proc mappings` that holds it, plus the
/// versions of gdb and the kernel. Shown only when the test fails.
fn where_in_core(dump: &[u8], offsets: &[usize], gdb_stdout: &str) -> String {
    let u16_at = |at: usize| u16::from_le_bytes([dump[at], dump[at + 1]]);
    let u32_at = |at: usize| u32::from_le_bytes(dump[at..at + 4].try_into().expect("4 bytes"));
    let u64_at = |at: usize| u64::from_le_bytes(dump[at..at + 8].try_into().expect("8 bytes"));
    let as_usize = |v: u64| usize::try_from(v).expect("fits usize");
    // ELF64 header: e_phoff at 0x20, e_phentsize at 0x36, e_phnum at 0x38.
    let phoff = as_usize(u64_at(0x20));
    let phentsize = usize::from(u16_at(0x36));
    let phnum = usize::from(u16_at(0x38));
    let mut lines = Vec::new();
    for &off in offsets {
        let mut place = format!("offset {off:#x}: outside every segment");
        for i in 0..phnum {
            let ph = phoff + i * phentsize;
            let (kind, flags) = (u32_at(ph), u32_at(ph + 4));
            let (file_off, vaddr, size) = (
                as_usize(u64_at(ph + 8)),
                u64_at(ph + 16),
                as_usize(u64_at(ph + 32)),
            );
            if off < file_off || off >= file_off + size {
                continue;
            }
            place = match kind {
                1 => {
                    let addr = vaddr + u64::try_from(off - file_off).expect("fits u64");
                    let mapping = mapping_of(gdb_stdout, addr).unwrap_or("not in the mappings");
                    format!("offset {off:#x}: LOAD flags {flags:#x}, address {addr:#x}, {mapping}")
                }
                4 => format!("offset {off:#x}: NOTE (registers or other process state)"),
                other => format!("offset {off:#x}: segment type {other}"),
            };
            break;
        }
        lines.push(place);
    }
    let gdb = Command::new("gdb")
        .arg("--version")
        .output()
        .map(|o| {
            String::from_utf8_lossy(&o.stdout)
                .lines()
                .next()
                .unwrap_or("")
                .to_owned()
        })
        .unwrap_or_default();
    let kernel = fs::read_to_string("/proc/sys/kernel/osrelease").unwrap_or_default();
    format!(
        "{}\ngdb: {gdb}\nkernel: {}",
        lines.join("\n"),
        kernel.trim()
    )
}

/// The line of `info proc mappings` in `gdb_stdout` whose range holds `addr`.
fn mapping_of(gdb_stdout: &str, addr: u64) -> Option<&str> {
    gdb_stdout.lines().find(|line| {
        let mut fields = line.split_whitespace();
        let start = fields
            .next()
            .and_then(|f| u64::from_str_radix(f.trim_start_matches("0x"), 16).ok());
        let end = fields
            .next()
            .and_then(|f| u64::from_str_radix(f.trim_start_matches("0x"), 16).ok());
        matches!((start, end), (Some(s), Some(e)) if s <= addr && addr < e)
    })
}

#[test]
#[cfg_attr(gdb_missing, ignore = "gdb not found")]
fn freed_marker_not_in_core() {
    let control = marker_copies_in_core(false);
    assert!(
        control.count > 0,
        "control run without zeroing must keep the marker"
    );
    let zeroed = marker_copies_in_core(true);
    assert_eq!(
        zeroed.count, 0,
        "freed marker found in the core:\n{}",
        zeroed.report
    );
}
