//! `smartnote-search`: entry point of the search worker.

use std::io;
use std::process::ExitCode;

#[global_allocator]
static ALLOC: zalloc::Zeroizing = zalloc::Zeroizing;

fn main() -> ExitCode {
    worker::panic::install_hook();
    // Before any thread exists; `hello` repeats it with the parent pid (spec §8.1).
    if let Err(err) = worker::harden::harden_process(None) {
        eprintln!("harden failed: {err}");
        return ExitCode::FAILURE;
    }
    worker::run(io::stdin(), io::stdout())
}
