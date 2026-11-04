//! Ranking of the search results (spec §6.7, §6.8): caps per note, the semantic thresholds,
//! Reciprocal Rank Fusion of the branch lists and snippets with highlight marks.
//!
//! Every ordering here is total: equal scores fall back to the note order (internal numbers
//! in the order notes were first indexed) and the line, so a result never depends on hash map
//! iteration or on the history of the word index.

use std::cmp::Ordering;
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
/// Lowest dot product of a chunk with the query that the semantic list takes (`τ_confirm`,
/// spec §6.7): a semantic line on a line of the word or substring list adds its RRF term.
pub const TAU_CONFIRM: f32 = 0.80;
/// Lowest chunk score of a line that only the semantic list found for it to enter the results
/// (`τ_alone`, spec §6.7).
pub const TAU_ALONE: f32 = 0.84;
/// Most lines that only the semantic list found in the results (spec §6.7).
pub const SEMANTIC_ONLY_MAX: usize = 3;
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
    /// Chunk score of the line in the semantic list, if it is there.
    pub semantic: Option<f32>,
}

/// Orders `hits` by score (larger first), then by note and line.
pub fn sort_by_score(hits: &mut [LineHit]) {
    hits.sort_by(|a, b| by_score((a.score, a.note, a.line), (b.score, b.note, b.line)));
}

/// The order of ranked lines given as `(score, note, line)`: score (larger first), then note
/// and line.
fn by_score(a: (f32, NoteKey, u32), b: (f32, NoteKey, u32)) -> Ordering {
    b.0.total_cmp(&a.0).then(a.1.cmp(&b.1)).then(a.2.cmp(&b.2))
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
/// lines are ordered by score, then note and line, and taken in that order: a line that only
/// the semantic list found is taken only with a chunk score of at least `alone` and while
/// fewer than [`SEMANTIC_ONLY_MAX`] such lines are taken; then at most [`PER_NOTE`] lines of a
/// note and `limit` lines in all. A line left out takes no place of its note or of the limit.
#[must_use]
pub fn fuse(lists: &[(Source, &[LineHit])], limit: usize, alone: f32) -> Vec<Fused> {
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
                    semantic: None,
                });
                fused.len() - 1
            });
            let entry = &mut fused[idx];
            entry.score += rrf(i + 1);
            if !entry.sources.contains(&source) {
                entry.sources.push(source);
            }
            if source == Source::Semantic {
                entry.semantic = Some(entry.semantic.map_or(hit.score, |s| s.max(hit.score)));
            }
        }
    }
    fused.sort_by(|a, b| by_score((a.score, a.note, a.line), (b.score, b.note, b.line)));

    let mut taken: HashMap<NoteKey, usize> = HashMap::new();
    let mut semantic_only = 0;
    let mut out = Vec::new();
    for f in fused {
        if out.len() >= limit {
            break;
        }
        let alone_line = f.sources == [Source::Semantic];
        if alone_line
            && (semantic_only >= SEMANTIC_ONLY_MAX || !f.semantic.is_some_and(|s| s >= alone))
        {
            continue;
        }
        let count = taken.entry(f.note).or_default();
        if *count >= PER_NOTE {
            continue;
        }
        *count += 1;
        semantic_only += usize::from(alone_line);
        out.push(f);
    }
    out
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

/// Snippet of `line`: at most [`SNIPPET_CHARS`] characters around the first of `marks` (UTF-16
/// ranges in `line`, clamped to it), or from the start of the line without one, with the parts
/// of all marks inside the snippet as UTF-16 ranges of the snippet, sorted, overlaps merged. The
/// snippet is an exact substring of `line`, cut on character boundaries; the frontend adds any
/// ellipsis.
#[must_use]
pub fn snippet(line: &str, marks: &[Range<u32>]) -> (String, Vec<[u32; 2]>) {
    // (byte, UTF-16) offsets of every character start and of the end.
    let mut bounds: Vec<(usize, u32)> = Vec::with_capacity(line.len() + 1);
    let mut units = 0_u32;
    for (byte, c) in line.char_indices() {
        bounds.push((byte, units));
        units += u32::try_from(c.len_utf16()).unwrap_or(2);
    }
    bounds.push((line.len(), units));
    let chars = bounds.len() - 1;

    let clamped: Vec<Range<u32>> = marks
        .iter()
        .map(|r| r.start.min(units)..r.end.min(units))
        .filter(|r| r.start < r.end)
        .collect();
    let (from, to) = match clamped.first() {
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
    let mut inside: Vec<[u32; 2]> = clamped
        .iter()
        .map(|r| {
            [
                r.start.max(from_units).min(to_units) - from_units,
                r.end.min(to_units).saturating_sub(from_units),
            ]
        })
        .filter(|[s, e]| s < e)
        .collect();
    inside.sort_unstable();
    let mut merged: Vec<[u32; 2]> = Vec::with_capacity(inside.len());
    for [s, e] in inside {
        match merged.last_mut() {
            Some(last) if s < last[1] => last[1] = last[1].max(e),
            _ => merged.push([s, e]),
        }
    }
    (line[from_byte..to_byte].to_owned(), merged)
}

#[cfg(test)]
#[expect(
    clippy::single_range_in_vec_init,
    reason = "a slice of one range is one mark, the argument of `snippet`"
)]
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
            TAU_ALONE,
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
        let fused = fuse(
            &[(Source::Words, &a), (Source::Semantic, &b)],
            10,
            TAU_ALONE,
        );
        let order: Vec<_> = fused.iter().map(|f| (f.note, f.line)).collect();
        assert_eq!(order, [(3, 7), (5, 2)]);
    }

    fn lines(fused: &[Fused]) -> Vec<(NoteKey, u32)> {
        fused.iter().map(|f| (f.note, f.line)).collect()
    }

    #[test]
    fn fuse_confirming_semantic_boosts_lexical() {
        let words = [hit(1, 0, 9.0), hit(2, 4, 5.0)];
        // Below τ_alone, but on a line of the word list: it confirms that line.
        let semantic = [hit(2, 4, 0.81)];
        let fused = fuse(
            &[(Source::Words, &words), (Source::Semantic, &semantic)],
            10,
            TAU_ALONE,
        );
        assert_eq!(lines(&fused), [(2, 4), (1, 0)]);
        assert_eq!(fused[0].sources, [Source::Words, Source::Semantic]);
        assert!((fused[0].score - (1.0 / 62.0 + 1.0 / 61.0)).abs() < 1e-7);
    }

    #[test]
    fn fuse_semantic_only_needs_tau_alone() {
        let words = [hit(1, 0, 9.0)];
        let semantic = [hit(2, 1, 0.84), hit(3, 1, 0.83)];
        let fused = fuse(
            &[(Source::Words, &words), (Source::Semantic, &semantic)],
            10,
            TAU_ALONE,
        );
        assert_eq!(lines(&fused), [(1, 0), (2, 1)], "0.83 dropped, 0.84 kept");
        assert_eq!(fused[1].sources, [Source::Semantic]);
    }

    #[test]
    fn fuse_semantic_only_at_most_three() {
        let words = [hit(9, 1, 9.0)];
        let semantic: Vec<LineHit> = (1..=5).map(|n| hit(n, 1, 0.9)).collect();
        let fused = fuse(
            &[(Source::Words, &words), (Source::Semantic, &semantic)],
            10,
            TAU_ALONE,
        );
        let only = fused
            .iter()
            .filter(|f| f.sources == [Source::Semantic])
            .count();
        assert_eq!(only, SEMANTIC_ONLY_MAX);
        assert_eq!(lines(&fused), [(1, 1), (9, 1), (2, 1), (3, 1)]);
    }

    #[test]
    fn fuse_dropped_semantic_takes_no_place() {
        // Note 1: a semantic-only line below τ_alone between its lexical lines.
        let words = [hit(1, 1, 9.0), hit(1, 2, 8.0), hit(1, 3, 7.0)];
        let semantic = [hit(1, 5, 0.82)];
        let fused = fuse(
            &[(Source::Words, &words), (Source::Semantic, &semantic)],
            3,
            TAU_ALONE,
        );
        assert_eq!(lines(&fused), [(1, 1), (1, 2), (1, 3)]);
    }

    #[test]
    fn fuse_nonsense_without_lexical_is_empty() {
        let semantic = [hit(1, 1, 0.83), hit(2, 3, 0.81), hit(3, 2, 0.80)];
        let fused = fuse(&[(Source::Semantic, &semantic)], 10, TAU_ALONE);
        assert_eq!(fused, Vec::<Fused>::new());
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
            snippet("short needle", &[6..12]),
            ("short needle".to_owned(), vec![[6, 12]])
        );
        assert_eq!(snippet("short", &[]), ("short".to_owned(), Vec::new()));
        assert_eq!(snippet("", &[0..4]), (String::new(), Vec::new()));
    }

    #[test]
    fn snippet_window_edges() {
        let line = "x".repeat(300);
        let (s, marks) = snippet(&line, &[0..3]);
        assert_eq!((s.len(), marks), (160, vec![[0, 3]]));
        let (s, marks) = snippet(&line, &[297..300]);
        assert_eq!((s.len(), marks), (160, vec![[157, 160]]));
        // A mark longer than the snippet starts it and is clipped.
        let (s, marks) = snippet(&line, &[100..290]);
        assert_eq!((s.len(), marks), (160, vec![[0, 160]]));
        // Without a mark: the start of the line.
        assert_eq!(snippet(&line, &[]).0.len(), 160);
    }

    #[test]
    fn snippet_surrogates() {
        let line = format!("{}needle{}", "😀".repeat(200), "😀".repeat(200));
        let start = 400;
        let (s, marks) = snippet(&line, &[start..start + 6]);
        assert_eq!(s.chars().count(), 160);
        let units: Vec<u16> = s.encode_utf16().collect();
        let [a, b] = marks[0];
        assert_eq!(
            String::from_utf16_lossy(&units[a as usize..b as usize]),
            "needle"
        );
    }

    #[test]
    fn snippet_marks_every_range() {
        assert_eq!(
            snippet("needle and needle", &[0..6, 11..17]),
            ("needle and needle".to_owned(), vec![[0, 6], [11, 17]])
        );
        // Unsorted and overlapping marks come out sorted and merged.
        assert_eq!(
            snippet("abcdef", &[3..5, 0..2, 1..3]).1,
            vec![[0, 3], [3, 5]]
        );
    }

    #[test]
    fn snippet_window_follows_the_first_mark() {
        let line = format!(
            "{}aim{}far{}",
            "x".repeat(100),
            "y".repeat(50),
            "z".repeat(300)
        );
        let (s, marks) = snippet(&line, &[100..103, 153..156, 400..403]);
        assert_eq!(s.chars().count(), 160);
        let from = line.find(&s).expect("a substring of the line");
        let units = |[a, b]: [u32; 2]| &s[a as usize..b as usize];
        assert!(
            from <= 100 && from + 160 >= 103,
            "the window holds the first mark"
        );
        assert_eq!(
            marks.iter().map(|&m| units(m)).collect::<Vec<_>>(),
            ["aim", "far"],
            "the marks inside the window, not the one beyond it"
        );
    }
}
