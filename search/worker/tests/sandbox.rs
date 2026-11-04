//! Sandbox of the worker on Linux (spec §10.2, §12.5): the probe applies the same function as
//! the worker and reports which operations the kernel still allows.
#![cfg(all(feature = "test-probes", target_os = "linux"))]

use std::collections::BTreeMap;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::sync::atomic::{AtomicUsize, Ordering};

/// A fresh directory with `existing.txt`, removed on drop.
struct ProbeDir(PathBuf);

impl ProbeDir {
    fn new() -> Self {
        static NEXT: AtomicUsize = AtomicUsize::new(0);
        let name = format!(
            "smartnote-sandbox-{}-{}",
            std::process::id(),
            NEXT.fetch_add(1, Ordering::Relaxed)
        );
        let dir = std::env::temp_dir().join(name);
        fs::create_dir(&dir).expect("create probe dir");
        fs::write(dir.join("existing.txt"), "probe").expect("write existing file");
        Self(dir)
    }
}

impl Drop for ProbeDir {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

/// Runs `probe_sandbox [off] <dir>` and returns its `name=result` lines.
fn run_probe(off: bool, dir: &Path) -> BTreeMap<String, String> {
    let output = Command::new(env!("CARGO_BIN_EXE_probe_sandbox"))
        .args(off.then_some("off"))
        .arg(dir)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .output()
        .expect("probe runs");
    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(
        output.status.success(),
        "{:?}\nstdout: {stdout}\nstderr: {}",
        output.status,
        String::from_utf8_lossy(&output.stderr)
    );
    stdout
        .lines()
        .map(|line| {
            let (name, result) = line.split_once('=').expect("name=result line");
            (name.to_owned(), result.to_owned())
        })
        .collect()
}

#[test]
fn sandbox_blocks_writes_and_network() {
    let dir = ProbeDir::new();
    let results = run_probe(false, &dir.0);
    let expected = [
        ("create_file", "EACCES"),
        ("open_existing_for_write", "EACCES"),
        ("tcp_connect_127.0.0.1:9", "EACCES"),
        ("udp_socket", "EPERM"),
        ("io_uring_setup", "EPERM"),
        ("read_file", "ok"),
        ("extra_fds_include_socket", "yes"),
        ("level", "full"),
    ];
    for (name, result) in expected {
        assert_eq!(
            results.get(name).map(String::as_str),
            Some(result),
            "{name}; all results: {results:?}"
        );
    }
    assert!(!dir.0.join("new.txt").exists(), "the file was created");
    assert_eq!(
        fs::read_to_string(dir.0.join("existing.txt")).expect("read existing file"),
        "probe"
    );
}

#[test]
fn sandbox_control_without_apply() {
    let dir = ProbeDir::new();
    let results = run_probe(true, &dir.0);
    for name in [
        "create_file",
        "open_existing_for_write",
        "udp_socket",
        "io_uring_setup",
        "read_file",
    ] {
        assert_eq!(
            results.get(name).map(String::as_str),
            Some("ok"),
            "{name}; all results: {results:?}"
        );
    }
    // Nothing listens on the port: the kernel lets the connection reach the stack.
    let tcp = results.get("tcp_connect_127.0.0.1:9").map(String::as_str);
    assert!(
        matches!(tcp, Some("ok" | "ECONNREFUSED")),
        "tcp_connect: {tcp:?}"
    );
    assert!(!results.contains_key("level"), "{results:?}");
    assert!(dir.0.join("new.txt").exists(), "the file was not created");
}
