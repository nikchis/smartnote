//! Substring search: a linear scan of search forms (spec §6.6).

use std::cmp::Reverse;
use std::collections::HashMap;
use std::ops::Range;

use memchr::memmem;

use super::{LineHit, NoteKey};

/// Finds `needle` in the search forms `lines` of `(note, line, form)`.
///
/// Lines rank by the number of occurrences (`score`), then by note and line. At most `per_note`
/// lines of a note and `k` lines in all are taken, the cap applied while walking the ranking so
/// that a note with many matches does not crowd out the others. An empty needle finds nothing.
#[must_use]
pub fn find_substring<'a>(
    lines: impl Iterator<Item = (NoteKey, u32, &'a str)>,
    needle: &str,
    per_note: usize,
    k: usize,
) -> Vec<LineHit> {
    if needle.is_empty() {
        return Vec::new();
    }
    let finder = memmem::Finder::new(needle.as_bytes());
    let mut found: Vec<(usize, LineHit)> = lines
        .filter_map(|(note, line, form)| {
            let mut at = finder.find_iter(form.as_bytes());
            let first = at.next()?;
            let count = 1 + at.count();
            #[expect(clippy::cast_precision_loss, reason = "counts are far below 2^24")]
            let score = count as f32;
            Some((
                count,
                LineHit {
                    note,
                    line,
                    score,
                    first: Some(first),
                },
            ))
        })
        .collect();
    found.sort_unstable_by_key(|(count, hit)| (Reverse(*count), hit.note, hit.line));

    let mut per_note_taken: HashMap<NoteKey, usize> = HashMap::new();
    let mut hits = Vec::new();
    for (_, hit) in found {
        if hits.len() >= k {
            break;
        }
        let taken = per_note_taken.entry(hit.note).or_default();
        if *taken < per_note {
            *taken += 1;
            hits.push(hit);
        }
    }
    hits
}

/// Byte ranges in `form` of every occurrence of `needle`, from the start, without overlaps
/// (the highlight of a substring hit). An empty needle has none.
#[must_use]
pub fn substring_ranges(form: &str, needle: &str) -> Vec<Range<usize>> {
    if needle.is_empty() {
        return Vec::new();
    }
    memmem::find_iter(form.as_bytes(), needle.as_bytes())
        .map(|at| at..at + needle.len())
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn lines<'a>(forms: &[(NoteKey, &'a str)]) -> Vec<(NoteKey, u32, &'a str)> {
        forms
            .iter()
            .enumerate()
            .map(|(i, &(note, form))| (note, u32::try_from(i).expect("small"), form))
            .collect()
    }

    #[test]
    fn substring_operators() {
        let text = "if (a == b) { x->y }";
        for (needle, at) in [("==", 6), ("(", 3), ("->", 15), ("{", 12)] {
            let hits = find_substring(lines(&[(1, text)]).into_iter(), needle, 3, 50);
            assert_eq!(hits.len(), 1, "needle {needle}");
            assert_eq!(hits[0].first, Some(at), "needle {needle}");
        }
    }

    #[test]
    fn substring_cap_per_note() {
        let mut forms = vec![(1, "->> ->>"); 60];
        forms.push((2, "->>"));
        forms.push((3, "->>"));
        let hits = find_substring(lines(&forms).into_iter(), "->>", 3, 50);
        let count = |n| hits.iter().filter(|h| h.note == n).count();
        assert_eq!((count(1), count(2), count(3)), (3, 1, 1));
    }

    #[test]
    fn substring_offsets_in_long_line() {
        let text = format!("{}needle", "a".repeat(8000));
        let hits = find_substring(lines(&[(1, &text)]).into_iter(), "needle", 3, 50);
        assert_eq!(hits[0].first, Some(8000));
    }

    #[test]
    fn substring_ranks_by_occurrences_then_order() {
        let forms = [(2, "ab"), (1, "ab ab"), (1, "ab")];
        let hits = find_substring(lines(&forms).into_iter(), "ab", 3, 50);
        let order: Vec<_> = hits.iter().map(|h| (h.note, h.line)).collect();
        assert_eq!(order, [(1, 1), (1, 2), (2, 0)]);
        assert!((hits[0].score - 2.0).abs() < f32::EPSILON);
    }

    #[test]
    fn substring_empty_needle_and_input() {
        assert_eq!(
            find_substring(lines(&[(1, "x")]).into_iter(), "", 3, 50).len(),
            0
        );
        assert_eq!(find_substring(std::iter::empty(), "abc", 3, 50).len(), 0);
    }

    #[test]
    fn substring_ranges_every_occurrence() {
        assert_eq!(substring_ranges("a == b == c", "=="), [2..4, 7..9]);
        assert_eq!(
            substring_ranges("aaaa", "aa"),
            [0..2, 2..4],
            "not overlapping"
        );
        assert_eq!(substring_ranges("abc", "x"), Vec::<Range<usize>>::new());
        assert_eq!(substring_ranges("abc", ""), Vec::<Range<usize>>::new());
    }
}
