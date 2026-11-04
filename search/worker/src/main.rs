//! `smartnote-search`: entry point of the search worker.

use std::io;
use std::process::ExitCode;

#[global_allocator]
static ALLOC: zalloc::Zeroizing = zalloc::Zeroizing;

#[allow(
    unsafe_code,
    reason = "std::env::set_var and closing inherited descriptors while single-threaded"
)]
fn main() -> ExitCode {
    // ONNX Runtime must not start its telemetry client (`worker::embed::load_ort` refuses to
    // load otherwise), whatever environment Smartnote passes.
    // SAFETY: the first statement of `main`, so the process is single-threaded: the Rust runtime
    // starts no thread before `main`, and the only constructors that run earlier (`inventory`
    // registrations of tantivy's `typetag`) push static nodes onto a list and start no thread.
    // No other thread can read or write the environment concurrently.
    unsafe { std::env::set_var(worker::embed::DISABLE_TELEMETRY, "1") };
    // Descriptors Smartnote leaked without O_CLOEXEC would bypass the sandbox (spec §10.2 p. 1).
    // On failure the check before `ready` reports what stayed open.
    // SAFETY: nothing of the process owns a descriptor above 2 yet: the runtime opens none
    // before `main`, and the statements above open none.
    if let Err(err) = unsafe { worker::harden::close_inherited_fds() } {
        eprintln!("closing inherited descriptors failed: {err}");
    }
    worker::panic::install_hook();
    // Before any thread exists; `hello` repeats it with the parent pid (spec §8.1).
    if let Err(err) = worker::harden::harden_process(None) {
        eprintln!("harden failed: {err}");
        return ExitCode::FAILURE;
    }
    worker::run(io::stdin(), io::stdout())
}
