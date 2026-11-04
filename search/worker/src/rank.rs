//! Ranking of the search results (spec §6.7, §6.8): caps per note, the semantic threshold,
//! Reciprocal Rank Fusion of the branch lists and snippets with highlight marks.
//!
//! Every ordering here is total: equal scores fall back to the note order (internal numbers
//! in the order notes were first indexed) and the line, so a result never depends on hash map
//! iteration or on the history of the word index.

use std::collections::HashMap;
use std::ops::Range;

use crate::embed::DIM;
use crate::index::{LineHit, NoteKey};
use crate::protocol::messages::Source;

/// Lines of one note per branch list and in the final results.
pub const PER_NOTE: usize = 3;
/// Lines per branch list.
pub const BRANCH_K: usize = 50;
/// Constant of Reciprocal Rank Fusion.
pub const RRF_K: f32 = 60.0;
/// Lowest dot product of a chunk with the query that the semantic list takes.
pub const TAU: f32 = 0.80;
/// Longest snippet, in characters (Unicode scalar values).
pub const SNIPPET_CHARS: usize = 160;

/// A line of the fused ranking.
#[derive(Clone, Debug, PartialEq)]
pub struct Fused {
    pub note: NoteKey,
    pub line: u32,
    /// Sum of the RRF terms of the line over the branch lists.
    pub score: f32,
    /// Branches that found the line, in the order of the lists given to [`fuse`].
    pub sources: Vec<Source>,
}

/// Orders `hits` by score (larger first), then by note and line.
pub fn sort_by_score(hits: &mut [LineHit]) {
    hits.sort_by(|a, b| {
        b.score
            .total_cmp(&a.score)
            .then(a.note.cmp(&b.note))
            .then(a.line.cmp(&b.line))
    });
}

/// Takes `items` in order, at most `per_note` of one note and `k` in all.
pub fn cap<T>(
    items: impl IntoIterator<Item = T>,
    note: impl Fn(&T) -> NoteKey,
    per_note: usize,
    k: usize,
) -> Vec<T> {
    let mut taken: HashMap<NoteKey, usize> = HashMap::new();
    let mut out = Vec::new();
    for item in items {
        if out.len() >= k {
            break;
        }
        let count = taken.entry(note(&item)).or_default();
        if *count < per_note {
            *count += 1;
            out.push(item);
        }
    }
    out
}

/// RRF term of a 1-based rank: `1 / (RRF_K + rank)`.
fn rrf(rank: usize) -> f32 {
    #[expect(clippy::cast_precision_loss, reason = "ranks are at most BRANCH_K")]
    let rank = rank as f32;
    1.0 / (RRF_K + rank)
}

/// Fuses the branch lists by Reciprocal Rank Fusion over lines: a line scores
/// `Σ 1 / (RRF_K + rank)` over the lists that contain it, `rank` counted from 1. The fused
/// lines are ordered by score, then note and line, and capped at [`PER_NOTE`] lines of a note
/// and `limit` lines in all.
#[must_use]
pub fn fuse(lists: &[(Source, &[LineHit])], limit: usize) -> Vec<Fused> {
    let mut fused: Vec<Fused> = Vec::new();
    let mut at: HashMap<(NoteKey, u32), usize> = HashMap::new();
    for &(source, list) in lists {
        for (i, hit) in list.iter().enumerate() {
            let idx = *at.entry((hit.note, hit.line)).or_insert_with(|| {
                fused.push(Fused {
                    note: hit.note,
                    line: hit.line,
                    score: 0.0,
                    sources: Vec::new(),
                });
                fused.len() - 1
            });
            let entry = &mut fused[idx];
            entry.score += rrf(i + 1);
            if !entry.sources.contains(&source) {
                entry.sources.push(source);
            }
        }
    }
    fused.sort_by(|a, b| {
        b.score
            .total_cmp(&a.score)
            .then(a.note.cmp(&b.note))
            .then(a.line.cmp(&b.line))
    });
    cap(fused, |f| f.note, PER_NOTE, limit)
}

/// RRF score of each line over the lexical lists alone: the measure of the best lexical line
/// in the range of a semantic chunk (spec §6.7).
#[must_use]
pub fn lexical_scores(lists: &[&[LineHit]]) -> HashMap<(NoteKey, u32), f32> {
    let mut scores = HashMap::new();
    for list in lists {
        for (i, hit) in list.iter().enumerate() {
            *scores.entry((hit.note, hit.line)).or_insert(0.0) += rrf(i + 1);
        }
    }
    scores
}

/// `v` scaled to unit length; `None` for a zero or non-finite vector.
#[must_use]
pub fn normalized(v: &[f32; DIM]) -> Option<[f32; DIM]> {
    let norm = v.iter().map(|x| x * x).sum::<f32>().sqrt();
    if !(norm.is_finite() && norm > 0.0) {
        return None;
    }
    Some(v.map(|x| x / norm))
}

/// Dot product of two vectors.
#[must_use]
pub fn dot(a: &[f32; DIM], b: &[f32; DIM]) -> f32 {
    a.iter().zip(b).map(|(x, y)| x * y).sum()
}

/// Snippet of `line`: at most [`SNIPPET_CHARS`] characters around `mark` (a UTF-16 range in
/// `line`, clamped to it), or from the start of the line without a mark, with the part of the
/// mark inside the snippet as a UTF-16 range of the snippet. The snippet is an exact substring
/// of `line`, cut on character boundaries; the frontend adds any ellipsis.
#[must_use]
pub fn snippet(line: &str, mark: Option<Range<u32>>) -> (String, Vec<[u32; 2]>) {
    // (byte, UTF-16) offsets of every character start and of the end.
    let mut bounds: Vec<(usize, u32)> = Vec::with_capacity(line.len() + 1);
    let mut units = 0_u32;
    for (byte, c) in line.char_indices() {
        bounds.push((byte, units));
        units += u32::try_from(c.len_utf16()).unwrap_or(2);
    }
    bounds.push((line.len(), units));
    let chars = bounds.len() - 1;

    let mark = mark
        .map(|r| r.start.min(units)..r.end.min(units))
        .filter(|r| r.start < r.end);
    let (from, to) = match &mark {
        _ if chars <= SNIPPET_CHARS => (0, chars),
        None => (0, SNIPPET_CHARS),
        Some(r) => {
            // Characters holding the first and the last unit of the mark.
            let first = bounds.partition_point(|&(_, u)| u <= r.start) - 1;
            let end = bounds.partition_point(|&(_, u)| u < r.end);
            let width = end - first;
            let from = first.saturating_sub(SNIPPET_CHARS.saturating_sub(width) / 2);
            let from = from.min(chars - SNIPPET_CHARS);
            (from, from + SNIPPET_CHARS)
        }
    };
    let ((from_byte, from_units), (to_byte, to_units)) = (bounds[from], bounds[to]);
    let marks = mark
        .map(|r| {
            [
                r.start.saturating_sub(from_units),
                r.end.min(to_units).saturating_sub(from_units),
            ]
        })
        .filter(|[s, e]| s < e)
        .into_iter()
        .collect();
    (line[from_byte..to_byte].to_owned(), marks)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn hit(note: NoteKey, line: u32, score: f32) -> LineHit {
        LineHit {
            note,
            line,
            score,
            first: None,
        }
    }

    #[test]
    fn fuse_rrf_one_based() {
        let words = [hit(1, 0, 9.0), hit(2, 1, 5.0)];
        let substring = [hit(2, 1, 3.0)];
        let fused = fuse(
            &[(Source::Words, &words), (Source::Substring, &substring)],
            10,
        );
        let order: Vec<_> = fused.iter().map(|f| (f.note, f.line)).collect();
        assert_eq!(order, [(2, 1), (1, 0)]);
        assert!((fused[0].score - (1.0 / 62.0 + 1.0 / 61.0)).abs() < 1e-7);
        assert!((fused[1].score - 1.0 / 61.0).abs() < 1e-7);
        assert_eq!(fused[0].sources, [Source::Words, Source::Substring]);
    }

    #[test]
    fn fuse_ties_follow_note_and_line() {
        let a = [hit(5, 2, 1.0)];
        let b = [hit(3, 7, 1.0)];
        let fused = fuse(&[(Source::Words, &a), (Source::Semantic, &b)], 10);
        let order: Vec<_> = fused.iter().map(|f| (f.note, f.line)).collect();
        assert_eq!(order, [(3, 7), (5, 2)]);
    }

    #[test]
    fn sort_by_score_breaks_ties() {
        let mut hits = [
            hit(2, 0, 1.0),
            hit(1, 3, 1.0),
            hit(1, 1, 1.0),
            hit(9, 9, 2.0),
        ];
        sort_by_score(&mut hits);
        let order: Vec<_> = hits.iter().map(|h| (h.note, h.line)).collect();
        assert_eq!(order, [(9, 9), (1, 1), (1, 3), (2, 0)]);
    }

    #[test]
    fn cap_per_note_and_total() {
        let items = [1, 1, 1, 1, 2, 3, 3];
        assert_eq!(cap(items, |&n| n, 3, 10), [1, 1, 1, 2, 3, 3]);
        assert_eq!(cap(items, |&n| n, 1, 2), [1, 2]);
    }

    #[test]
    fn normalized_rejects_degenerate() {
        assert_eq!(normalized(&[0.0; DIM]), None);
        let mut v = [0.0; DIM];
        v[0] = f32::NAN;
        assert_eq!(normalized(&v), None);
        v[0] = 3.0;
        v[1] = 4.0;
        let n = normalized(&v).expect("unit");
        assert!((dot(&n, &n) - 1.0).abs() < 1e-6);
    }

    #[test]
    fn snippet_short_line_whole() {
        assert_eq!(
            snippet("short needle", Some(6..12)),
            ("short needle".to_owned(), vec![[6, 12]])
        );
        assert_eq!(snippet("short", None), ("short".to_owned(), Vec::new()));
        assert_eq!(snippet("", Some(0..4)), (String::new(), Vec::new()));
    }

    #[test]
    fn snippet_window_edges() {
        let line = "x".repeat(300);
        let (s, marks) = snippet(&line, Some(0..3));
        assert_eq!((s.len(), marks), (160, vec![[0, 3]]));
        let (s, marks) = snippet(&line, Some(297..300));
        assert_eq!((s.len(), marks), (160, vec![[157, 160]]));
        // A mark longer than the snippet starts it and is clipped.
        let (s, marks) = snippet(&line, Some(100..290));
        assert_eq!((s.len(), marks), (160, vec![[0, 160]]));
        // Without a mark: the start of the line.
        assert_eq!(snippet(&line, None).0.len(), 160);
    }

    #[test]
    fn snippet_surrogates() {
        let line = format!("{}needle{}", "😀".repeat(200), "😀".repeat(200));
        let start = 400;
        let (s, marks) = snippet(&line, Some(start..start + 6));
        assert_eq!(s.chars().count(), 160);
        let units: Vec<u16> = s.encode_utf16().collect();
        let [a, b] = marks[0];
        assert_eq!(
            String::from_utf16_lossy(&units[a as usize..b as usize]),
            "needle"
        );
    }
}
