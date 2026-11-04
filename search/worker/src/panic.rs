//! Panic hook that reports only the location of a panic (spec §6.2).

use std::io::{self, Write};
use std::panic::Location;

/// Replaces the panic hook: stderr gets `panic at <file>:<line>:<col>` and nothing else.
/// Smartnote logs the file only when it is one of this crate's sources (`workerSourceFiles` in
/// `internal/adapter/searchworker/stderr.go`, checked by a Go test), else `<other>`, and a few
/// such lines per run (keep the two in step).
///
/// The payload is left out on purpose: messages of the standard library (for example a
/// string slice off a char boundary) quote the data they failed on, which may be note text.
pub fn install_hook() {
    std::panic::set_hook(Box::new(|info| {
        // Nothing to report to if stderr is gone; the process aborts after the hook anyway.
        let _ = write_location(&mut io::stderr().lock(), info.location());
    }));
}

fn write_location(out: &mut impl Write, location: Option<&Location<'_>>) -> io::Result<()> {
    match location {
        Some(l) => writeln!(out, "panic at {}:{}:{}", l.file(), l.line(), l.column()),
        None => writeln!(out, "panic at <unknown>"),
    }
}

#[cfg(test)]
mod tests {
    use super::write_location;
    use std::panic::Location;

    #[test]
    fn writes_file_line_and_column() {
        let location = Location::caller();
        let mut out = Vec::new();
        write_location(&mut out, Some(location)).expect("write to Vec");
        let expected = format!(
            "panic at {}:{}:{}\n",
            location.file(),
            location.line(),
            location.column()
        );
        assert_eq!(String::from_utf8(out).expect("UTF-8"), expected);
    }

    #[test]
    fn unknown_location_is_reported_as_such() {
        let mut out = Vec::new();
        write_location(&mut out, None).expect("write to Vec");
        assert_eq!(out, b"panic at <unknown>\n");
    }
}
