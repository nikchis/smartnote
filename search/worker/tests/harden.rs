//! Hardening of the worker on Linux (spec §10.1, §8.1, §12.5). The probe runs as a child,
//! so with `kernel.yama.ptrace_scope=1` the test may read its memory unless it is hardened.
#![cfg(all(feature = "test-probes", target_os = "linux"))]

use std::fs::{self, File};
use std::io::{BufRead, BufReader, ErrorKind};
use std::process::{Child, Command, Stdio};
use std::thread;
use std::time::{Duration, Instant};

/// A running `probe_zalloc sleep`, killed on drop.
struct Probe(Child);

impl Probe {
    /// Starts `probe_zalloc sleep <arg>`.
    fn start(arg: Option<&str>) -> Self {
        let mut cmd = Command::new(env!("CARGO_BIN_EXE_probe_zalloc"));
        cmd.arg("sleep").args(arg).stdout(Stdio::piped());
        Self(cmd.spawn().expect("probe starts"))
    }

    /// Starts the probe and waits until it has hardened itself (or skipped it).
    fn ready(arg: Option<&str>) -> Self {
        let mut probe = Self::start(arg);
        let stdout = probe.0.stdout.take().expect("piped stdout");
        let mut line = String::new();
        BufReader::new(stdout)
            .read_line(&mut line)
            .expect("read probe stdout");
        assert_eq!(line, "ready\n", "probe status: {:?}", probe.0.try_wait());
        probe
    }

    fn proc_path(&self, name: &str) -> String {
        format!("/proc/{}/{name}", self.0.id())
    }
}

impl Drop for Probe {
    fn drop(&mut self) {
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}

fn own_pid() -> String {
    std::process::id().to_string()
}

#[test]
fn child_memory_is_not_readable() {
    let hardened = Probe::ready(Some(&own_pid()));
    let err = File::open(hardened.proc_path("mem")).expect_err("memory of a hardened probe");
    assert_eq!(err.kind(), ErrorKind::PermissionDenied);

    let limits = fs::read_to_string(hardened.proc_path("limits")).expect("read limits");
    let core: Vec<&str> = limits
        .lines()
        .find_map(|l| l.strip_prefix("Max core file size"))
        .expect("core limit line")
        .split_whitespace()
        .collect();
    assert_eq!(core[..2], ["0", "0"], "limits: {limits}");

    let control = Probe::ready(Some("off"));
    File::open(control.proc_path("mem")).expect("control run: memory is readable");
}

#[test]
fn child_runs_with_lowered_priority() {
    let hardened = Probe::ready(Some(&own_pid()));
    let stat = fs::read_to_string(hardened.proc_path("stat")).expect("read stat");
    let after_comm = &stat[stat.rfind(')').expect("comm ends with ')'") + 1..];
    // Field 19 of /proc/<pid>/stat; the text after `comm` starts with field 3.
    let nice: i32 = after_comm
        .split_whitespace()
        .nth(16)
        .and_then(|f| f.parse().ok())
        .expect("nice field");
    assert!(nice >= 10, "nice {nice}");
}

#[test]
fn exits_on_parent_mismatch() {
    let mut probe = Probe::start(Some("1"));
    let deadline = Instant::now() + Duration::from_secs(1);
    let status = loop {
        if let Some(status) = probe.0.try_wait().expect("wait for probe") {
            break status;
        }
        assert!(Instant::now() < deadline, "probe still runs after 1 s");
        thread::sleep(Duration::from_millis(10));
    };
    assert!(!status.success(), "{status:?}");
}
