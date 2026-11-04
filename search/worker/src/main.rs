//! `smartnote-search`: entry point of the search worker.

use std::io;
use std::process::ExitCode;

#[global_allocator]
static ALLOC: zalloc::Zeroizing = zalloc::Zeroizing;

fn main() -> ExitCode {
    worker::panic::install_hook();
    // The parent pid arrives with `hello` (task 9); until then the parent-death signal is set
    // without the `getppid` check.
    if let Err(err) = worker::harden::harden_process(None) {
        eprintln!("harden failed: {err}");
        return ExitCode::FAILURE;
    }
    match worker::run(io::stdin().lock()) {
        Ok(()) => ExitCode::SUCCESS,
        Err(err) => {
            eprintln!("stdin read failed: {err}");
            ExitCode::FAILURE
        }
    }
}
