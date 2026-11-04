//! The search worker `smartnote-search`: a child process of Smartnote that indexes notes
//! and answers search queries over stdin/stdout (spec §6).

pub mod harden;
pub mod index;
pub mod panic;
pub mod protocol;
pub mod sandbox;
pub mod text;

use std::io::{self, Read};

/// Serves the parent until `input` (stdin) reaches EOF.
///
/// Placeholder until the worker loop (task 9): reads and discards the input.
///
/// # Errors
///
/// Returns the read error of `input`.
pub fn run(mut input: impl Read) -> io::Result<()> {
    io::copy(&mut input, &mut io::sink())?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use std::io::Cursor;

    #[test]
    fn run_returns_at_eof() {
        super::run(Cursor::new(b"frame".to_vec())).expect("EOF ends the run");
    }
}
