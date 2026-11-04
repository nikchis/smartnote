//! Marks the build `gdb_missing` when the dump test of feature `test-probes` cannot run
//! (`tests/zalloc_dump.rs` is then ignored instead of failing).

use std::env;
use std::process::{Command, Stdio};

fn main() {
    println!("cargo::rustc-check-cfg=cfg(gdb_missing)");
    println!("cargo::rerun-if-env-changed=PATH");
    if env::var_os("CARGO_FEATURE_TEST_PROBES").is_none() {
        return;
    }
    let gdb = Command::new("gdb")
        .arg("--version")
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();
    if !gdb.is_ok_and(|s| s.success()) {
        println!("cargo::rustc-cfg=gdb_missing");
    }
}
