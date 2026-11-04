//! Word and substring search over the lines of notes (spec §6.5, §6.6, §6.8).

pub mod lexical;
pub mod substring;

pub use lexical::LexicalIndex;
pub use substring::find_substring;

/// Internal number of a note.
pub type NoteKey = u64;

/// A line found by one kind of search.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct LineHit {
    /// Note of the line.
    pub note: NoteKey,
    /// Number of the line in the note; line 0 is the title.
    pub line: u32,
    /// Rank of the line within its kind of search; larger is better.
    pub score: f32,
    /// Byte offset of the first match in the search form of the line.
    pub first: usize,
}
