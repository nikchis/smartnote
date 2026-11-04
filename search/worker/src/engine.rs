//! The note table of the worker and the search over it (spec §3.3 p. 3, §6.2, §6.7, §6.8).
//!
//! [`Engine`] owns the text of every note, the search forms of its lines, its embeddings and
//! the word index. It holds no I/O and never logs: note text stays in memory.

use std::collections::{HashMap, HashSet};
use std::ops::Range;

use crate::embed::{ChunkVec, DIM, Record, text_hash};
use crate::index::{LexicalIndex, LineHit, NoteKey, find_substring};
use crate::protocol::messages::{Hit, NoteIn, Source};
use crate::rank::{self, BRANCH_K, Fused, PER_NOTE, TAU_ALONE, TAU_CONFIRM};
use crate::text::{Query, SearchForm, search_form};

/// Result of an accepted [`Engine::upsert`].
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Upserted {
    /// The note is indexed with its embeddings (possibly an empty record: a note without
    /// non-empty lines has no chunks).
    Fresh,
    /// The note is indexed for words and substrings; its embeddings must be computed.
    NeedsEmbedding {
        /// Internal number of the note.
        key: NoteKey,
    },
}

/// Notes in memory with their word index and embeddings (spec §6.2, the index thread).
///
/// Revisions: every id keeps the last applied `rev` of this generation, removals included, so
/// a late `upsert` older than a `remove` is ignored (spec §3.3 p. 3). [`Engine::reset`] starts
/// a new generation and forgets them.
///
/// Word index failures: tantivy errors of a RAM index mean a broken writer. The engine then
/// drops the word index ([`Engine::lexical_ready`] turns `false`); substring and semantic
/// search keep working, and [`Engine::reset`] tries to build a new one.
pub struct Engine {
    index_id: [u8; 32],
    notes: HashMap<NoteKey, NoteEntry>,
    keys: HashMap<String, NoteKey>,
    revs: HashMap<String, u64>,
    /// Internal numbers are never reused, not even across generations, so a word index
    /// document of a removed note can never be taken for another note.
    next_key: NoteKey,
    lexical: Option<LexicalIndex>,
    /// `τ_confirm` and `τ_alone` (spec §6.7): [`TAU_CONFIRM`] and [`TAU_ALONE`] unless the
    /// calibration changes them.
    tau_confirm: f32,
    tau_alone: f32,
}

/// A note in memory. No `Debug`: it holds note text.
struct NoteEntry {
    id: String,
    rev: u64,
    name: String,
    info: String,
    /// Line 0 is the name, lines 1… are the lines of `info`.
    lines: Vec<LineEntry>,
    text_hash: [u8; 32],
    /// `None` until the note has embeddings for its current text.
    chunks: Option<Vec<ChunkVec>>,
}

/// A line of a note: its search form and where it lies in the name (line 0) or `info`.
struct LineEntry {
    form: SearchForm,
    /// Byte range in the source text.
    bytes: Range<u32>,
    /// UTF-16 offset of the line start in the source text.
    utf16: u32,
}

/// What a hit highlights.
enum Highlight {
    /// A UTF-16 range within the line.
    Match(Range<u32>),
    /// Lines `first..=last` of `info`: a semantic chunk without lexical matches.
    Chunk { first: u32, last: u32 },
}

/// The semantic branch list with the chunk range of every line taken without a lexical match.
#[derive(Default)]
struct SemanticList {
    hits: Vec<LineHit>,
    chunks: HashMap<(NoteKey, u32), (u32, u32)>,
}

impl Engine {
    /// Creates an empty engine whose embedding records carry `index_id` (spec §4.2).
    #[must_use]
    pub fn new(index_id: [u8; 32]) -> Self {
        Self {
            index_id,
            notes: HashMap::new(),
            keys: HashMap::new(),
            revs: HashMap::new(),
            next_key: 0,
            lexical: LexicalIndex::new().ok(),
            tau_confirm: TAU_CONFIRM,
            tau_alone: TAU_ALONE,
        }
    }

    /// Sets the semantic thresholds (spec §6.7): `confirm`, the lowest chunk score the semantic
    /// list takes ([`TAU_CONFIRM`] by default), and `alone`, the lowest chunk score of a line
    /// only the semantic list found ([`TAU_ALONE`] by default). Used by the calibration
    /// (`tests/bench.rs`); the worker keeps the defaults.
    pub fn set_semantic_thresholds(&mut self, confirm: f32, alone: f32) {
        self.tau_confirm = confirm;
        self.tau_alone = alone;
    }

    /// Whether word search is available (see the type documentation).
    #[must_use]
    pub fn lexical_ready(&self) -> bool {
        self.lexical.is_some()
    }

    /// Adds or replaces a note; `None` if `n.rev` is not newer than the last applied revision
    /// of the id (an upsert or a removal). Word index changes take effect at
    /// [`commit`](Self::commit).
    ///
    /// The stored record `n.emb` is taken only when it decodes and was made for this index
    /// version and exactly this name and text; otherwise the note needs new embeddings.
    pub fn upsert(&mut self, n: NoteIn) -> Option<Upserted> {
        if self.is_stale(&n.id, n.rev) {
            return None;
        }
        self.revs.insert(n.id.clone(), n.rev);
        let key = if let Some(&key) = self.keys.get(&n.id) {
            key
        } else {
            let key = self.next_key;
            self.next_key += 1;
            self.keys.insert(n.id.clone(), key);
            key
        };
        let hash = text_hash(&n.name, &n.info);
        let chunks = n
            .emb
            .as_deref()
            .and_then(|b| Record::decode(b))
            .filter(|r| r.index == self.index_id && r.text == hash)
            .map(|r| r.chunks);
        let lines = note_lines(&n.name, &n.info);
        let forms: Vec<&str> = lines.iter().map(|l| l.form.text.as_str()).collect();
        self.with_lexical(|index| index.replace_note(key, &forms));
        let upserted = if chunks.is_some() {
            Upserted::Fresh
        } else {
            Upserted::NeedsEmbedding { key }
        };
        self.notes.insert(
            key,
            NoteEntry {
                id: n.id,
                rev: n.rev,
                name: n.name,
                info: n.info,
                lines,
                text_hash: hash,
                chunks,
            },
        );
        Some(upserted)
    }

    /// Removes a note; `false` if `rev` is not newer than the last applied revision of the id
    /// or there is no such note. A newer `rev` is remembered either way. Takes effect in the
    /// word index at [`commit`](Self::commit).
    pub fn remove(&mut self, id: &str, rev: u64) -> bool {
        if self.is_stale(id, rev) {
            return false;
        }
        self.revs.insert(id.to_owned(), rev);
        let Some(key) = self.keys.remove(id) else {
            return false;
        };
        self.notes.remove(&key);
        self.with_lexical(|index| index.remove_note(key));
        true
    }

    /// Drops every note and every remembered revision: Smartnote follows with a full rebuild
    /// (spec §3.3 p. 4). The word index is cleared at once.
    pub fn reset(&mut self) {
        self.notes.clear();
        self.keys.clear();
        self.revs.clear();
        if self.lexical.is_some() {
            self.with_lexical(|index| {
                index.clear()?;
                index.commit()
            });
        } else {
            self.lexical = LexicalIndex::new().ok();
        }
    }

    /// Makes word index changes visible to [`search`](Self::search).
    pub fn commit(&mut self) {
        self.with_lexical(LexicalIndex::commit);
    }

    /// Stores the embeddings of note `id` computed for revision `rev`; returns the encoded
    /// record for Smartnote to store, or `None` if the note is gone or has a newer revision.
    pub fn set_chunks(&mut self, id: &str, rev: u64, chunks: Vec<ChunkVec>) -> Option<Vec<u8>> {
        let entry = self.keys.get(id).and_then(|k| self.notes.get_mut(k))?;
        if entry.rev != rev {
            return None;
        }
        let record = Record {
            index: self.index_id,
            text: entry.text_hash,
            chunks,
        };
        let bytes = record.encode();
        entry.chunks = Some(record.chunks);
        Some(bytes)
    }

    /// Revision, name and lines of `info` of a note that still needs embeddings; `None` if the
    /// note is gone or already has them.
    #[must_use]
    pub fn note_for_embedding(&self, id: &str) -> Option<(u64, String, Vec<String>)> {
        let entry = self.keys.get(id).and_then(|k| self.notes.get(k))?;
        if entry.chunks.is_some() {
            return None;
        }
        let lines = entry.info.split('\n').map(str::to_owned).collect();
        Some((entry.rev, entry.name.clone(), lines))
    }

    /// Searches the notes (spec §6.8): the word, substring and semantic lists, each with at
    /// most [`PER_NOTE`] lines of a note and [`BRANCH_K`] lines; Reciprocal Rank Fusion; again
    /// at most [`PER_NOTE`] lines of a note; at most `limit` hits.
    ///
    /// The semantic list runs when `q` has semantic text and `qvec` is given: chunks whose dot
    /// product with the normalized `qvec` is at least `τ_confirm`. A line only the semantic list
    /// found enters the results with a chunk score of at least `τ_alone`, at most
    /// [`SEMANTIC_ONLY_MAX`](rank::SEMANTIC_ONLY_MAX) of them (see [`rank::fuse`]).
    #[must_use]
    pub fn search(&self, q: &Query, qvec: Option<&[f32; DIM]>, limit: usize) -> Vec<Hit> {
        if limit == 0 {
            return Vec::new();
        }
        let words = self.word_list(q);
        let substring = q.substring.as_deref().map_or_else(Vec::new, |needle| {
            find_substring(self.forms(), needle, PER_NOTE, BRANCH_K)
        });
        let semantic = match (&q.semantic, qvec) {
            (Some(_), Some(qvec)) => self.semantic_list(qvec, &words, &substring),
            _ => SemanticList::default(),
        };
        let fused = rank::fuse(
            &[
                (Source::Words, &words),
                (Source::Substring, &substring),
                (Source::Semantic, &semantic.hits),
            ],
            limit,
            self.tau_alone,
        );
        fused
            .into_iter()
            .filter_map(|f| self.hit(f, q, &substring, &semantic))
            .collect()
    }

    /// Number of notes and of notes without embeddings.
    #[must_use]
    pub fn counts(&self) -> (u32, u32) {
        let pending = self.notes.values().filter(|n| n.chunks.is_none()).count();
        (saturate(self.notes.len()), saturate(pending))
    }

    fn is_stale(&self, id: &str, rev: u64) -> bool {
        self.revs.get(id).is_some_and(|&applied| rev <= applied)
    }

    /// Runs `f` on the word index; on failure drops the index (see the type documentation).
    fn with_lexical(&mut self, f: impl FnOnce(&mut LexicalIndex) -> tantivy::Result<()>) {
        if let Some(index) = &mut self.lexical
            && f(index).is_err()
        {
            self.lexical = None;
        }
    }

    /// `(note, line, form)` of every line of every note.
    fn forms(&self) -> impl Iterator<Item = (NoteKey, u32, &str)> {
        self.notes.iter().flat_map(|(&key, note)| {
            (0_u32..)
                .zip(&note.lines)
                .map(move |(line, l)| (key, line, l.form.text.as_str()))
        })
    }

    /// The word list. Tantivy breaks ties of equal BM25 scores by document address, which
    /// depends on the history of the index; the list is re-sorted by score, note and line
    /// before the caps, so equal lines rank in note order. Lines of notes changed after the
    /// last commit are dropped when they no longer exist. A search failure (not expected of a
    /// RAM index) gives an empty list.
    fn word_list(&self, q: &Query) -> Vec<LineHit> {
        let Some(index) = &self.lexical else {
            return Vec::new();
        };
        let Ok(mut hits) = index.search(q, usize::MAX, usize::MAX) else {
            return Vec::new();
        };
        hits.retain(|h| {
            self.notes
                .get(&h.note)
                .is_some_and(|n| (h.line as usize) < n.lines.len())
        });
        rank::sort_by_score(&mut hits);
        rank::cap(hits, |h| h.note, PER_NOTE, BRANCH_K)
    }

    /// The semantic list (spec §6.7, §6.8): every chunk at or above `τ_confirm`, best first (ties
    /// by note and chunk order), placed on a line: the best line of the chunk found by the
    /// word or substring list, otherwise the first non-empty line with the chunk remembered
    /// for the highlight. A line is taken once, by its best chunk.
    fn semantic_list(
        &self,
        qvec: &[f32; DIM],
        words: &[LineHit],
        substring: &[LineHit],
    ) -> SemanticList {
        let Some(qvec) = rank::normalized(qvec) else {
            return SemanticList::default();
        };
        let mut scored: Vec<(f32, NoteKey, &NoteEntry, &ChunkVec)> = self
            .notes
            .iter()
            .flat_map(|(&key, note)| {
                note.chunks
                    .iter()
                    .flatten()
                    .map(move |chunk| (rank::dot(&qvec, &chunk.vec), key, note, chunk))
            })
            .filter(|&(score, ..)| score >= self.tau_confirm)
            .collect();
        // Chunks of a note are compared by position: `first`, then `last`.
        scored.sort_by(|a, b| {
            b.0.total_cmp(&a.0)
                .then(a.1.cmp(&b.1))
                .then((a.3.first, a.3.last).cmp(&(b.3.first, b.3.last)))
        });

        let lexical = rank::lexical_scores(&[words, substring]);
        let mut seen = HashSet::new();
        let mut chunks = HashMap::new();
        let mut hits = Vec::new();
        for (score, key, note, chunk) in scored {
            let Some((first, last)) = note.chunk_lines(chunk) else {
                continue;
            };
            let best_lexical = (first..=last)
                .filter_map(|line| lexical.get(&(key, line)).map(|&s| (line, s)))
                .max_by(|a, b| a.1.total_cmp(&b.1).then(b.0.cmp(&a.0)));
            let line = if let Some((line, _)) = best_lexical {
                line
            } else {
                let line = (first..=last)
                    .find(|&l| !note.source_line(l).trim().is_empty())
                    .unwrap_or(first);
                chunks.entry((key, line)).or_insert((first, last));
                line
            };
            if seen.insert((key, line)) {
                hits.push(LineHit {
                    note: key,
                    line,
                    score,
                    first: None,
                });
            }
        }
        SemanticList {
            hits: rank::cap(hits, |h| h.note, PER_NOTE, BRANCH_K),
            chunks,
        }
    }

    /// Builds the hit of a fused line; `None` if the line no longer exists.
    fn hit(
        &self,
        f: Fused,
        q: &Query,
        substring: &[LineHit],
        semantic: &SemanticList,
    ) -> Option<Hit> {
        let note = self.notes.get(&f.note)?;
        let line = note.lines.get(f.line as usize)?;
        let highlight = self.highlight(&f, line, q, substring, semantic);
        let source = note.source_line(f.line);
        let text_of_line = if f.line == 0 { &note.name } else { &note.info };
        let (start, end, text, (snippet, marks)) = match highlight {
            Highlight::Match(r) => {
                let bytes = utf16_to_byte(source, r.start)..utf16_to_byte(source, r.end);
                (
                    line.utf16 + r.start,
                    line.utf16 + r.end,
                    source.get(bytes).unwrap_or_default().to_owned(),
                    rank::snippet(source, Some(r)),
                )
            }
            Highlight::Chunk { first, last } => {
                let a = note.lines.get(first as usize)?;
                let b = note.lines.get(last as usize)?;
                let end = b.utf16 + b.form.utf16_at(b.form.text.len());
                let bytes = a.bytes.start as usize..b.bytes.end as usize;
                (
                    a.utf16,
                    end,
                    text_of_line.get(bytes).unwrap_or_default().to_owned(),
                    rank::snippet(source, None),
                )
            }
        };
        Some(Hit {
            note: note.id.clone(),
            line: f.line,
            start,
            end,
            text,
            snippet,
            marks,
            sources: f.sources,
            score: f.score,
        })
    }

    /// What the hit of a fused line highlights: the substring match, else the word match (the
    /// line start when the analyzer finds none), else the semantic chunk.
    fn highlight(
        &self,
        f: &Fused,
        line: &LineEntry,
        q: &Query,
        substring: &[LineHit],
        semantic: &SemanticList,
    ) -> Highlight {
        let form = &line.form;
        let by_substring = substring
            .iter()
            .find(|h| h.note == f.note && h.line == f.line)
            .and_then(|h| Some((h.first?, q.substring.as_deref()?.len())));
        if let Some((first, len)) = by_substring {
            return Highlight::Match(form.utf16_at(first)..form.utf16_end_at(first + len));
        }
        if f.sources.contains(&Source::Words) {
            let range = self
                .lexical
                .as_ref()
                .and_then(|index| index.match_range(&form.text, q))
                .map_or(0..0, |r| form.utf16_at(r.start)..form.utf16_end_at(r.end));
            return Highlight::Match(range);
        }
        match semantic.chunks.get(&(f.note, f.line)) {
            Some(&(first, last)) => Highlight::Chunk { first, last },
            None => Highlight::Match(0..0),
        }
    }
}

impl NoteEntry {
    /// Source text of a line: the name for line 0, a line of `info` otherwise.
    fn source_line(&self, line: u32) -> &str {
        let text = if line == 0 { &self.name } else { &self.info };
        self.lines
            .get(line as usize)
            .and_then(|l| text.get(l.bytes.start as usize..l.bytes.end as usize))
            .unwrap_or_default()
    }

    /// Lines of `info` a chunk covers, clamped to the note; `None` if none remain.
    fn chunk_lines(&self, chunk: &ChunkVec) -> Option<(u32, u32)> {
        let last_line = saturate(self.lines.len().saturating_sub(1));
        let (first, last) = (chunk.first.max(1), chunk.last.min(last_line));
        (first <= last).then_some((first, last))
    }
}

/// Lines of a note: the name, then `info` split by `\n`.
fn note_lines(name: &str, info: &str) -> Vec<LineEntry> {
    let mut lines = vec![LineEntry {
        form: search_form(name),
        bytes: 0..saturate(name.len()),
        utf16: 0,
    }];
    let (mut byte, mut utf16) = (0_usize, 0_usize);
    for line in info.split('\n') {
        lines.push(LineEntry {
            form: search_form(line),
            bytes: saturate(byte)..saturate(byte + line.len()),
            utf16: saturate(utf16),
        });
        byte += line.len() + 1;
        utf16 += line.encode_utf16().count() + 1;
    }
    lines
}

/// Byte offset in `s` of the UTF-16 offset `units`, rounded down to a character start.
fn utf16_to_byte(s: &str, units: u32) -> usize {
    let mut seen = 0;
    for (byte, c) in s.char_indices() {
        seen += c.len_utf16();
        if seen > units as usize {
            return byte;
        }
    }
    s.len()
}

/// Sizes above `u32::MAX` do not occur: a frame is at most 16 MiB.
fn saturate(n: usize) -> u32 {
    u32::try_from(n).unwrap_or(u32::MAX)
}

#[cfg(test)]
mod tests {
    use serde_bytes::ByteBuf;

    use super::*;
    use crate::text::parse_query;

    const INDEX: [u8; 32] = [7; 32];

    fn note(id: &str, rev: u64, name: &str, info: &str) -> NoteIn {
        NoteIn {
            id: id.to_owned(),
            rev,
            name: name.to_owned(),
            info: info.to_owned(),
            emb: None,
        }
    }

    fn engine(notes: Vec<NoteIn>) -> Engine {
        let mut e = Engine::new(INDEX);
        for n in notes {
            assert!(e.upsert(n).is_some(), "fresh rev");
        }
        e.commit();
        e
    }

    fn search(e: &Engine, q: &str) -> Vec<Hit> {
        e.search(&parse_query(q), None, 10)
    }

    /// A unit vector whose dot product with `axis(0)` is `cos`.
    fn at_cos(cos: f32) -> [f32; DIM] {
        let mut v = [0.0; DIM];
        v[0] = cos;
        v[1] = (1.0 - cos * cos).sqrt();
        v
    }

    fn axis(i: usize) -> [f32; DIM] {
        let mut v = [0.0; DIM];
        v[i] = 1.0;
        v
    }

    fn chunk(first: u32, last: u32, vec: &[f32; DIM]) -> ChunkVec {
        ChunkVec {
            first,
            last,
            vec: *vec,
        }
    }

    /// UTF-16 slice `[start, end)` of `s`.
    fn utf16_slice(s: &str, start: u32, end: u32) -> String {
        let units: Vec<u16> = s.encode_utf16().collect();
        String::from_utf16(&units[start as usize..end as usize]).expect("whole characters")
    }

    fn record(name: &str, info: &str, index: [u8; 32], chunks: Vec<ChunkVec>) -> ByteBuf {
        ByteBuf::from(
            Record {
                index,
                text: text_hash(name, info),
                chunks,
            }
            .encode(),
        )
    }

    #[test]
    fn upsert_ignores_stale_rev() {
        let mut e = engine(vec![note("a", 5, "new name", "fresh text")]);
        assert_eq!(e.upsert(note("a", 3, "old name", "stale text")), None);
        assert_eq!(e.upsert(note("a", 5, "old name", "stale text")), None);
        e.commit();
        assert_eq!(search(&e, "fresh").len(), 1);
        assert_eq!(search(&e, "stale"), Vec::<Hit>::new());
        assert!(!e.remove("a", 4));
        assert!(!e.remove("a", 5));
        assert_eq!(search(&e, "fresh").len(), 1);
    }

    #[test]
    fn remove_keeps_tombstone_until_reset() {
        let mut e = engine(vec![note("a", 5, "n", "alpha")]);
        assert!(e.remove("a", 6));
        e.commit();
        assert_eq!(search(&e, "alpha"), Vec::<Hit>::new());
        assert_eq!(
            e.upsert(note("a", 6, "n", "alpha")),
            None,
            "older than removal"
        );
        assert!(e.upsert(note("a", 7, "n", "alpha")).is_some());
        e.commit();
        assert_eq!(search(&e, "alpha").len(), 1);

        e.reset();
        assert_eq!(search(&e, "alpha"), Vec::<Hit>::new());
        assert_eq!(e.counts(), (0, 0));
        assert!(
            e.upsert(note("a", 1, "n", "alpha")).is_some(),
            "fresh generation"
        );
        e.commit();
        assert_eq!(search(&e, "alpha").len(), 1);
    }

    #[test]
    fn upsert_reuses_valid_record() {
        let mut e = Engine::new(INDEX);
        let mut n = note("a", 1, "name", "line one\nline two");
        n.emb = Some(record(&n.name, &n.info, INDEX, vec![chunk(1, 2, &axis(0))]));
        assert_eq!(e.upsert(n), Some(Upserted::Fresh));
        assert_eq!(e.counts(), (1, 0));
        assert_eq!(e.note_for_embedding("a"), None, "already embedded");

        // A note without non-empty lines has no chunks: its empty record is valid.
        let mut empty = note("b", 2, "title only", "");
        empty.emb = Some(record("title only", "", INDEX, Vec::new()));
        assert_eq!(e.upsert(empty), Some(Upserted::Fresh));
        assert_eq!(e.counts(), (2, 0));
    }

    #[test]
    fn upsert_rejects_foreign_record() {
        let mut e = Engine::new(INDEX);
        let mut other_index = note("a", 1, "name", "text");
        other_index.emb = Some(record("name", "text", [8; 32], vec![chunk(1, 1, &axis(0))]));
        assert!(matches!(
            e.upsert(other_index),
            Some(Upserted::NeedsEmbedding { .. })
        ));

        let mut other_text = note("b", 2, "name", "text");
        other_text.emb = Some(record("name", "other", INDEX, vec![chunk(1, 1, &axis(0))]));
        assert!(matches!(
            e.upsert(other_text),
            Some(Upserted::NeedsEmbedding { .. })
        ));

        let mut garbage = note("c", 3, "name", "text");
        garbage.emb = Some(ByteBuf::from(vec![0xff, 0x00]));
        assert!(matches!(
            e.upsert(garbage),
            Some(Upserted::NeedsEmbedding { .. })
        ));
        assert_eq!(e.counts(), (3, 3));
        assert_eq!(
            e.note_for_embedding("a"),
            Some((1, "name".to_owned(), vec!["text".to_owned()]))
        );
    }

    #[test]
    fn set_chunks_only_for_current_rev() {
        let mut e = engine(vec![note("a", 4, "name", "one\ntwo")]);
        assert_eq!(e.counts(), (1, 1));
        assert_eq!(e.set_chunks("a", 3, vec![chunk(1, 2, &axis(0))]), None);
        assert_eq!(e.set_chunks("missing", 4, Vec::new()), None);
        let bytes = e
            .set_chunks("a", 4, vec![chunk(1, 2, &axis(0))])
            .expect("current rev");
        let rec = Record::decode(&bytes).expect("valid record");
        assert_eq!(rec.index, INDEX);
        assert_eq!(rec.text, text_hash("name", "one\ntwo"));
        assert_eq!(rec.chunks, vec![chunk(1, 2, &axis(0))]);
        assert_eq!(e.counts(), (1, 0));
    }

    #[test]
    fn search_name_hit() {
        let e = engine(vec![note("a", 1, "Мой Пароль", "текст")]);
        let hits = search(&e, "пароль");
        assert_eq!(hits.len(), 1);
        let h = &hits[0];
        assert_eq!((h.note.as_str(), h.line, h.start, h.end), ("a", 0, 4, 10));
        assert_eq!(h.text, "Пароль");
        assert_eq!(h.snippet, "Мой Пароль");
        assert_eq!(h.marks, vec![[4, 10]]);
    }

    #[test]
    fn search_offsets_whole_info() {
        let e = engine(vec![note("a", 1, "name", "a\nxx needle")]);
        let hits = search(&e, "needle");
        assert_eq!(hits.len(), 1);
        let h = &hits[0];
        assert_eq!((h.line, h.start, h.end), (2, 5, 11));
        assert_eq!(h.text, "needle");
        assert_eq!(h.sources, vec![Source::Words, Source::Substring]);
        assert_eq!(h.snippet, "xx needle");
        assert_eq!(h.marks, vec![[3, 9]]);
    }

    #[test]
    fn search_offsets_surrogates_and_nfd() {
        let info = "😀\nЕ\u{0308}жик и\u{0306}од";
        let e = engine(vec![note("a", 1, "name", info)]);
        let hits = search(&e, "ёжик");
        assert_eq!(hits.len(), 1);
        let h = &hits[0];
        assert_eq!((h.line, h.start, h.end), (2, 3, 8));
        assert_eq!(h.text, "Е\u{0308}жик");
        assert_eq!(utf16_slice(info, h.start, h.end), h.text);

        let h = &search(&e, "йод")[0];
        assert_eq!(utf16_slice(info, h.start, h.end), "и\u{0306}од");
        assert_eq!(
            utf16_slice(&h.snippet, h.marks[0][0], h.marks[0][1]),
            "и\u{0306}од"
        );
    }

    #[test]
    fn search_empty_query() {
        let e = engine(vec![note("a", 1, "name", "text")]);
        assert_eq!(search(&e, ""), Vec::<Hit>::new());
        assert_eq!(search(&e, "  \"\" "), Vec::<Hit>::new());
    }

    #[test]
    fn search_semantic_threshold() {
        let mut e = engine(vec![
            note("a", 1, "first", "alpha line"),
            note("b", 2, "second", "beta line"),
        ]);
        e.set_chunks("a", 1, vec![chunk(1, 1, &at_cos(0.79))]);
        e.set_chunks("b", 2, vec![chunk(1, 1, &at_cos(0.85))]);
        let q = parse_query("unrelated query");
        let hits = e.search(&q, Some(&axis(0)), 10);
        assert_eq!(hits.len(), 1, "only the chunk above the threshold");
        assert_eq!((hits[0].note.as_str(), hits[0].line), ("b", 1));
        assert_eq!(hits[0].sources, vec![Source::Semantic]);

        assert_eq!(
            e.search(&q, Some(&axis(2)), 10),
            Vec::<Hit>::new(),
            "all below"
        );
        assert_eq!(e.search(&q, None, 10), Vec::<Hit>::new(), "no query vector");
    }

    #[test]
    fn search_semantic_thresholds() {
        let mut e = engine(vec![
            note("a", 1, "first", "alpha line"),
            note("b", 2, "second", "beta needle"),
        ]);
        e.set_chunks("a", 1, vec![chunk(1, 1, &at_cos(0.83))]);
        e.set_chunks("b", 2, vec![chunk(1, 1, &at_cos(0.81))]);
        // Only semantics, all below τ_alone: nothing (spec §6.7, `No matches`).
        let q = parse_query("unrelated query");
        assert_eq!(e.search(&q, Some(&axis(0)), 10), Vec::<Hit>::new());
        // At τ_confirm the chunk of b confirms its word match.
        let hits = e.search(&parse_query("needle"), Some(&axis(0)), 10);
        assert_eq!(hits.len(), 1);
        assert_eq!(hits[0].note, "b");
        assert_eq!(
            hits[0].sources,
            vec![Source::Words, Source::Substring, Source::Semantic]
        );

        e.set_semantic_thresholds(0.80, 0.83);
        let hits = e.search(&q, Some(&axis(0)), 10);
        assert_eq!(hits.len(), 1, "a passes the lower τ_alone");
        assert_eq!(hits[0].note, "a");
        e.set_semantic_thresholds(0.82, 0.82);
        let hits = e.search(&parse_query("needle"), Some(&axis(0)), 10);
        assert_eq!(hits[0].sources, vec![Source::Words, Source::Substring]);
    }

    #[test]
    fn search_semantic_line_prefers_lexical() {
        let info = "intro\nother\n\nfour\nfive needle\nsix\nseven";
        let mut e = engine(vec![note("a", 1, "name", info)]);
        e.set_chunks("a", 1, vec![chunk(3, 6, &at_cos(0.9))]);

        let hits = e.search(&parse_query("needle"), Some(&axis(0)), 10);
        assert_eq!(hits.len(), 1);
        let h = &hits[0];
        assert_eq!(h.line, 5);
        assert_eq!(
            h.sources,
            vec![Source::Words, Source::Substring, Source::Semantic]
        );
        assert_eq!(h.text, "needle");

        let hits = e.search(&parse_query("unrelated"), Some(&axis(0)), 10);
        assert_eq!(hits.len(), 1);
        let h = &hits[0];
        assert_eq!(h.line, 4, "first non-empty line of the chunk");
        assert_eq!(h.sources, vec![Source::Semantic]);
        assert_eq!(
            h.text, "\nfour\nfive needle\nsix",
            "the whole chunk, lines 3–6"
        );
        assert_eq!(utf16_slice(info, h.start, h.end), h.text);
        assert_eq!(h.snippet, "four");
        assert_eq!(h.marks, Vec::<[u32; 2]>::new());
    }

    #[test]
    fn search_diversity_after_rrf() {
        let many = vec!["needle"; 60].join("\n");
        let e = engine(vec![
            note("a", 1, "name", &many),
            note("b", 2, "name", "needle"),
            note("c", 3, "name", "x needle"),
        ]);
        let hits = search(&e, "needle");
        let count = |id: &str| hits.iter().filter(|h| h.note == id).count();
        assert!(count("a") <= 3, "{}", count("a"));
        assert_eq!((count("b"), count("c")), (1, 1));
        assert!(hits.windows(2).all(|w| w[0].score >= w[1].score));
    }

    #[test]
    fn search_limit() {
        let notes = (0..20)
            .map(|i| note(&format!("n{i}"), i + 1, "name", "needle"))
            .collect();
        let e = engine(notes);
        assert_eq!(e.search(&parse_query("needle"), None, 5).len(), 5);
    }

    #[test]
    fn search_word_ties_follow_note_order() {
        let mut e = engine(
            (1..=4)
                .map(|i| note(&format!("n{i}"), i, "name", "alpha"))
                .collect(),
        );
        let order = |e: &Engine| -> Vec<String> {
            e.search(&parse_query("alpha"), None, 10)
                .into_iter()
                .map(|h| h.note)
                .collect()
        };
        let before = order(&e);
        assert_eq!(before, ["n1", "n2", "n3", "n4"]);
        // Re-indexing moves the documents of n1 to the end of the tantivy index.
        assert!(e.upsert(note("n1", 10, "name", "alpha")).is_some());
        e.commit();
        assert_eq!(order(&e), before);
    }

    #[test]
    fn search_snippet_and_marks() {
        let side = "я😀 word ".repeat(25);
        let line = format!("{side}needle{side}");
        assert!(line.chars().count() >= 400);
        let e = engine(vec![note("a", 1, "name", &format!("first\n{line}"))]);
        let hits = search(&e, "needle");
        assert_eq!(hits.len(), 1);
        let h = &hits[0];
        assert!(
            h.snippet.chars().count() <= 160,
            "{}",
            h.snippet.chars().count()
        );
        assert!(line.contains(&h.snippet));
        assert_eq!(h.marks.len(), 1);
        let [s, e] = h.marks[0];
        assert_eq!(utf16_slice(&h.snippet, s, e), "needle");
        // The match is near the middle of the snippet.
        let len = u32::try_from(h.snippet.encode_utf16().count()).expect("small");
        assert!(s > len / 4 && e < len * 3 / 4, "{s}..{e} of {len}");
    }
}
