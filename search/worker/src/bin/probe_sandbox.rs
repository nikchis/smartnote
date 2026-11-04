//! Test helper (feature `test-probes`): applies the worker's sandbox and tries the operations
//! it must deny (spec §12.5). Used by `tests/sandbox.rs`.
//!
//! - `probe_sandbox <dir>` — opens a TCP socket and starts a second thread, applies
//!   [`worker::sandbox::apply`], then tries each operation and prints `name=result` per line
//!   (`ok` or the errno name), followed by `extra_fds_include_socket=yes|no` and
//!   `level=full|partial|none`. The `thread_*` operations run in the thread that existed
//!   before the sandbox.
//! - `probe_sandbox off <dir>` — the same operations without the sandbox (control run).
//!
//! `<dir>` must hold a file `existing.txt`; the probe creates `new.txt` and `thread.txt` next
//! to it.
#![allow(
    unsafe_code,
    reason = "raw socket, connect, setxattr and io_uring_setup calls through libc; every block \
              is commented"
)]

use std::process::ExitCode;

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let args: Vec<&str> = args.iter().map(String::as_str).collect();
    match args.as_slice() {
        [dir] => run(dir, true),
        ["off", dir] => run(dir, false),
        _ => {
            eprintln!("usage: probe_sandbox [off] <dir>");
            ExitCode::from(2)
        }
    }
}

#[cfg(not(target_os = "linux"))]
fn run(_dir: &str, _sandbox: bool) -> ExitCode {
    eprintln!("probe_sandbox runs on Linux only");
    ExitCode::FAILURE
}

#[cfg(target_os = "linux")]
fn run(dir: &str, sandbox: bool) -> ExitCode {
    linux::run(std::path::Path::new(dir), sandbox)
}

#[cfg(target_os = "linux")]
mod linux {
    use std::ffi::CString;
    use std::fs::{self, File, OpenOptions};
    use std::io::{self, Write};
    use std::mem::size_of;
    use std::net::{Ipv4Addr, UdpSocket};
    use std::os::fd::{AsRawFd, FromRawFd, OwnedFd};
    use std::os::unix::ffi::OsStrExt;
    use std::os::unix::net::UnixStream;
    use std::path::Path;
    use std::process::ExitCode;
    use std::sync::mpsc;
    use std::thread;

    use worker::sandbox::{self, SandboxLevel};

    /// Port of the TCP connection attempt: `discard`, normally closed on a test machine.
    const TCP_PORT: u16 = 9;

    pub(super) fn run(dir: &Path, sandbox: bool) -> ExitCode {
        let existing = dir.join("existing.txt");
        // Opened before the sandbox, as a descriptor the worker might have leaked: Landlock
        // must still deny its `connect`, and `extra_open_fds` must report it.
        let tcp = match tcp_socket() {
            Ok(fd) => fd,
            Err(err) => {
                eprintln!("tcp socket: {err}");
                return ExitCode::FAILURE;
            }
        };
        // Started before the sandbox: the sandbox must reach threads that already run.
        let (start, started) = mpsc::channel::<()>();
        let thread_file = dir.join("thread.txt");
        let early_thread = thread::spawn(move || {
            started.recv().ok()?;
            Some([
                ("thread_create_file", outcome(File::create_new(thread_file))),
                (
                    "thread_udp_socket",
                    outcome(UdpSocket::bind((Ipv4Addr::LOCALHOST, 0))),
                ),
            ])
        });
        let level = sandbox.then(|| sandbox::apply().level);
        let thread_results = start
            .send(())
            .ok()
            .and_then(|()| early_thread.join().ok().flatten());
        let Some(thread_results) = thread_results else {
            eprintln!("the early thread failed");
            return ExitCode::FAILURE;
        };

        let results = [
            (
                "create_file",
                outcome(File::create_new(dir.join("new.txt"))),
            ),
            (
                "open_existing_for_write",
                outcome(OpenOptions::new().write(true).open(&existing)),
            ),
            ("tcp_connect_127.0.0.1:9", outcome(tcp_connect(&tcp))),
            (
                "udp_socket",
                outcome(UdpSocket::bind((Ipv4Addr::LOCALHOST, 0))),
            ),
            ("socketpair", outcome(UnixStream::pair())),
            ("io_uring_setup", outcome(io_uring_setup())),
            ("setxattr", outcome(set_xattr(&existing))),
            ("read_file", outcome(fs::read(&existing))),
        ];
        let socket_reported =
            sandbox::extra_open_fds().is_ok_and(|fds| fds.contains(&tcp.as_raw_fd()));

        let mut out = io::stdout().lock();
        let mut report = || -> io::Result<()> {
            for (name, result) in results.iter().chain(&thread_results) {
                writeln!(out, "{name}={result}")?;
            }
            let yes_no = if socket_reported { "yes" } else { "no" };
            writeln!(out, "extra_fds_include_socket={yes_no}")?;
            if let Some(level) = level {
                writeln!(out, "level={}", level_name(level))?;
            }
            out.flush()
        };
        match report() {
            Ok(()) => ExitCode::SUCCESS,
            Err(_) => ExitCode::FAILURE,
        }
    }

    fn level_name(level: SandboxLevel) -> &'static str {
        match level {
            SandboxLevel::Full => "full",
            SandboxLevel::Partial => "partial",
            SandboxLevel::None => "none",
        }
    }

    /// `ok` or the name of the errno of the failure.
    fn outcome<T>(result: io::Result<T>) -> String {
        match result {
            Ok(_) => "ok".to_owned(),
            Err(err) => match err.raw_os_error() {
                Some(libc::EACCES) => "EACCES".to_owned(),
                Some(libc::EPERM) => "EPERM".to_owned(),
                Some(libc::ECONNREFUSED) => "ECONNREFUSED".to_owned(),
                Some(libc::ENOTSUP) => "ENOTSUP".to_owned(),
                Some(code) => format!("errno{code}"),
                None => format!("error({err})"),
            },
        }
    }

    /// Converts the `-1` failure return of a libc call into the `errno` error.
    fn check(rc: libc::c_long) -> io::Result<libc::c_long> {
        if rc == -1 {
            Err(io::Error::last_os_error())
        } else {
            Ok(rc)
        }
    }

    fn tcp_socket() -> io::Result<OwnedFd> {
        // SAFETY: `socket` takes plain integers and touches no memory of the process.
        let fd = check(
            unsafe { libc::socket(libc::AF_INET, libc::SOCK_STREAM | libc::SOCK_CLOEXEC, 0) }
                .into(),
        )?;
        let fd = i32::try_from(fd).map_err(io::Error::other)?;
        // SAFETY: `fd` was just returned by `socket`, is open and owned by nobody else.
        Ok(unsafe { OwnedFd::from_raw_fd(fd) })
    }

    fn tcp_connect(socket: &OwnedFd) -> io::Result<()> {
        let addr = libc::sockaddr_in {
            sin_family: libc::sa_family_t::try_from(libc::AF_INET).map_err(io::Error::other)?,
            sin_port: TCP_PORT.to_be(),
            sin_addr: libc::in_addr {
                s_addr: u32::from(Ipv4Addr::LOCALHOST).to_be(),
            },
            sin_zero: [0; 8],
        };
        let len =
            libc::socklen_t::try_from(size_of::<libc::sockaddr_in>()).map_err(io::Error::other)?;
        // SAFETY: `socket` is an open descriptor; `addr` is a valid `sockaddr_in` of `len`
        // bytes that outlives the call and is only read.
        let rc = unsafe {
            libc::connect(
                socket.as_raw_fd(),
                (&raw const addr).cast::<libc::sockaddr>(),
                len,
            )
        };
        check(rc.into()).map(drop)
    }

    /// Sets the extended attribute `user.smartnote.probe` of `path`.
    fn set_xattr(path: &Path) -> io::Result<()> {
        let path = CString::new(path.as_os_str().as_bytes()).map_err(io::Error::other)?;
        let name = c"user.smartnote.probe";
        let value = b"probe";
        // SAFETY: `path` and `name` are NUL-terminated strings and `value` holds `value.len()`
        // bytes; all outlive the call and are only read.
        let rc = unsafe {
            libc::setxattr(
                path.as_ptr(),
                name.as_ptr(),
                value.as_ptr().cast(),
                value.len(),
                0,
            )
        };
        check(rc.into()).map(drop)
    }

    /// `struct io_uring_params` of the kernel: 120 bytes, zeroed on input.
    #[repr(C)]
    struct IoUringParams([u64; 15]);

    fn io_uring_setup() -> io::Result<OwnedFd> {
        let mut params = IoUringParams([0; 15]);
        let entries: libc::c_uint = 1;
        // SAFETY: `io_uring_setup(entries, params)` reads and writes the 120-byte
        // `io_uring_params` that `params` provides; it outlives the call.
        let fd =
            check(unsafe { libc::syscall(libc::SYS_io_uring_setup, entries, &raw mut params) })?;
        let fd = i32::try_from(fd).map_err(io::Error::other)?;
        // SAFETY: `fd` was just returned by `io_uring_setup`, is open and owned by nobody else.
        Ok(unsafe { OwnedFd::from_raw_fd(fd) })
    }
}
