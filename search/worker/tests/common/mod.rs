//! A running `smartnote-search` for the end-to-end tests: frames go to its stdin, replies come
//! from its stdout through a reader thread, stderr is collected whole.
#![allow(dead_code, reason = "each test crate uses a part of the helpers")]

use std::env;
use std::ffi::OsString;
use std::fs;
use std::io::Read;
use std::path::{Path, PathBuf};
use std::process::{Child, ChildStdin, Command, ExitStatus, Stdio};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::mpsc::{self, Receiver};
use std::thread::{self, JoinHandle};
use std::time::Duration;

use worker::protocol::{NoteIn, PROTO, Reply, Request, decode, encode, read_frame, write_frame};

/// Longest wait for one reply; the model loads within it.
pub const REPLY_TIMEOUT: Duration = Duration::from_secs(30);

/// Path of the worker binary.
pub const WORKER: &str = env!("CARGO_BIN_EXE_smartnote-search");

/// Prepared model directory and ONNX Runtime library from `SMARTNOTE_MODEL_DIR` and
/// `SMARTNOTE_ORT_LIB` (absolute paths), if both are set.
pub fn model_paths() -> Option<(PathBuf, PathBuf)> {
    Some((
        env::var_os("SMARTNOTE_MODEL_DIR")?.into(),
        env::var_os("SMARTNOTE_ORT_LIB")?.into(),
    ))
}

/// [`model_paths`] or a panic that names the variables.
pub fn required_model_paths() -> (PathBuf, PathBuf) {
    model_paths().expect(
        "SMARTNOTE_MODEL_DIR and SMARTNOTE_ORT_LIB must be set: task model:fetch, task ort:fetch",
    )
}

/// A fresh empty directory, removed on drop.
pub struct TempDir(pub PathBuf);

impl TempDir {
    pub fn new(tag: &str) -> Self {
        static NEXT: AtomicUsize = AtomicUsize::new(0);
        let name = format!(
            "smartnote-{tag}-{}-{}",
            std::process::id(),
            NEXT.fetch_add(1, Ordering::Relaxed)
        );
        let dir = env::temp_dir().join(name);
        fs::create_dir(&dir).expect("create temp dir");
        Self(dir)
    }

    pub fn path(&self) -> &Path {
        &self.0
    }
}

impl Drop for TempDir {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

/// The running worker, killed on drop.
pub struct Worker {
    child: Child,
    stdin: Option<ChildStdin>,
    replies: Receiver<Reply>,
    stderr: Option<JoinHandle<String>>,
    /// Pid the worker must see as its parent.
    pub parent_pid: u32,
    /// Empty model directory of a worker without the model, removed with the worker.
    empty_model: Option<TempDir>,
    /// `HOME` and `XDG_CACHE_HOME` of the worker.
    home: TempDir,
}

impl Worker {
    /// Starts the worker binary.
    pub fn start() -> Self {
        Self::spawn(Command::new(WORKER), false)
    }

    /// Starts `cmd`, which runs the worker either directly (the test is its parent) or, when
    /// `traced`, as the child of the started tracer.
    ///
    /// Like Smartnote, the harness passes no `ORT_*` variable of its own environment: only those
    /// `cmd` sets explicitly. So `ORT_DISABLE_TELEMETRY=1` of `make search-test` cannot stand in
    /// for the worker's own. `HOME` and `XDG_CACHE_HOME` point to a fresh directory, which
    /// [`Worker::wait`] checks for files of the ONNX Runtime telemetry.
    pub fn spawn(mut cmd: Command, traced: bool) -> Self {
        let explicit: Vec<OsString> = cmd.get_envs().map(|(key, _)| key.to_owned()).collect();
        for (key, _) in env::vars_os() {
            let inherited_ort = key.to_str().is_some_and(|k| k.starts_with("ORT_"));
            if inherited_ort && !explicit.contains(&key) {
                cmd.env_remove(&key);
            }
        }
        let home = TempDir::new("home");
        cmd.env("HOME", home.path())
            .env("XDG_CACHE_HOME", home.path().join(".cache"))
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        let mut child = cmd.spawn().expect("worker starts");
        let parent_pid = if traced {
            child.id()
        } else {
            std::process::id()
        };
        let mut stdout = child.stdout.take().expect("piped stdout");
        let (tx, replies) = mpsc::channel();
        thread::spawn(move || {
            while let Ok(Some(body)) = read_frame(&mut stdout) {
                let reply = decode::<Reply>(&body).expect("the worker writes valid replies");
                if tx.send(reply).is_err() {
                    break;
                }
            }
        });
        let mut stderr = child.stderr.take().expect("piped stderr");
        let stderr = thread::spawn(move || {
            let mut text = String::new();
            stderr.read_to_string(&mut text).expect("stderr is UTF-8");
            text
        });
        Self {
            stdin: child.stdin.take(),
            child,
            replies,
            stderr: Some(stderr),
            parent_pid,
            empty_model: None,
            home,
        }
    }

    /// Sends `hello` with the given model paths, or an empty model directory and a missing
    /// library, and returns the first reply.
    pub fn hello(&mut self, model: Option<(PathBuf, PathBuf)>) -> Reply {
        let (model_dir, ort_lib) = model.unwrap_or_else(|| {
            let empty = TempDir::new("no-model");
            let paths = (
                empty.path().to_path_buf(),
                PathBuf::from("/nonexistent/libonnxruntime.so"),
            );
            self.empty_model = Some(empty);
            paths
        });
        self.send(&Request::Hello {
            proto: PROTO,
            model_dir: model_dir.to_str().expect("UTF-8 path").into(),
            ort_lib: ort_lib.to_str().expect("UTF-8 path").into(),
            threads: 2,
            parent_pid: self.parent_pid,
        });
        self.recv()
    }

    pub fn send(&mut self, req: &Request) {
        let body = encode(req).expect("encode request");
        self.send_body(&body);
    }

    /// Writes one frame with `body` as is.
    pub fn send_body(&mut self, body: &[u8]) {
        let stdin = self.stdin.as_mut().expect("stdin is open");
        write_frame(stdin, body).expect("write frame");
    }

    /// Writes raw bytes, frame header included.
    pub fn send_raw(&mut self, bytes: &[u8]) {
        use std::io::Write;
        let stdin = self.stdin.as_mut().expect("stdin is open");
        stdin.write_all(bytes).expect("write raw bytes");
        stdin.flush().expect("flush");
    }

    /// Next reply, or a panic after [`REPLY_TIMEOUT`].
    pub fn recv(&self) -> Reply {
        self.try_recv(REPLY_TIMEOUT)
            .unwrap_or_else(|| panic!("no reply within {REPLY_TIMEOUT:?}"))
    }

    /// Next reply within `timeout`; `None` when there is none.
    pub fn try_recv(&self, timeout: Duration) -> Option<Reply> {
        self.replies.recv_timeout(timeout).ok()
    }

    /// Replies until the first one `want` accepts, which it returns; panics on timeout.
    pub fn recv_until<T>(&self, mut want: impl FnMut(Reply) -> Option<T>) -> T {
        loop {
            if let Some(found) = want(self.recv()) {
                return found;
            }
        }
    }

    /// Waits for `ok{req}`, skipping other replies.
    pub fn wait_ok(&self, req: u64) {
        self.recv_until(|r| matches!(r, Reply::Ok { req: got } if got == req).then_some(()));
    }

    /// Closes stdin (EOF for the worker).
    pub fn close_stdin(&mut self) {
        self.stdin = None;
    }

    /// Closes stdin, waits for the exit and returns its status with the whole stderr.
    pub fn finish(mut self) -> (ExitStatus, String) {
        self.close_stdin();
        self.wait()
    }

    /// Waits for the exit (stdin stays as it is) and returns its status with the whole stderr.
    /// Panics when the worker left any file in its `HOME`: ONNX Runtime telemetry writes
    /// `onnxruntime.db` and a device id under `.cache/Microsoft/DeveloperTools`.
    pub fn wait(mut self) -> (ExitStatus, String) {
        let status = wait_timeout(&mut self.child, REPLY_TIMEOUT);
        let left = files_under(self.home.path());
        assert!(left.is_empty(), "the worker wrote to its HOME: {left:#?}");
        let stderr = self
            .stderr
            .take()
            .expect("stderr is read once")
            .join()
            .expect("stderr reader");
        (status, stderr)
    }
}

impl Drop for Worker {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

/// Every file below `dir`, recursively.
pub fn files_under(dir: &Path) -> Vec<PathBuf> {
    let mut files = Vec::new();
    let mut dirs = vec![dir.to_path_buf()];
    while let Some(dir) = dirs.pop() {
        for entry in fs::read_dir(&dir).expect("read dir") {
            let path = entry.expect("dir entry").path();
            if path.is_dir() {
                dirs.push(path);
            } else {
                files.push(path);
            }
        }
    }
    files
}

/// Waits for `child` up to `timeout`; panics when it is still running.
fn wait_timeout(child: &mut Child, timeout: Duration) -> ExitStatus {
    let step = Duration::from_millis(10);
    let mut waited = Duration::ZERO;
    loop {
        if let Some(status) = child.try_wait().expect("try_wait") {
            return status;
        }
        assert!(
            waited < timeout,
            "the worker did not exit within {timeout:?}"
        );
        thread::sleep(step);
        waited += step;
    }
}

/// A note with a UUID-like id made from `n`.
pub fn note(n: u32, rev: u64, name: &str, info: &str) -> NoteIn {
    NoteIn {
        id: format!("00000000-0000-4000-8000-{n:012}"),
        rev,
        name: name.into(),
        info: info.into(),
        emb: None,
    }
}

/// `count` notes that carry `marker` in the name and the text, upserted in batches of 100
/// with requests numbered from `first_req`; returns the next free request number.
pub fn upsert_marked(w: &mut Worker, count: u32, marker: &str, first_req: u64) -> u64 {
    let notes: Vec<NoteIn> = (0..count)
        .map(|i| {
            note(
                i,
                1,
                &format!("{marker} note {i}"),
                &format!(
                    "first line {i} with {marker}\nretry backoff -> {i}s, a == b\nпароли и {marker}"
                ),
            )
        })
        .collect();
    let mut req = first_req;
    for batch in notes.chunks(100) {
        w.send(&Request::Upsert {
            req,
            notes: batch.to_vec(),
        });
        w.wait_ok(req);
        req += 1;
    }
    req
}
