//! Search form of a line (spec §6.3) and the map from it back to the source text.
//!
//! The form is NFC, then `ё`/`Ё` replaced with `е`/`Е`, then full Unicode lower case. Its
//! length differs from the source (NFD sequences from macOS compose, `İ` becomes `i̇`), so every
//! offset that leaves the worker is mapped back to UTF-16 code units of the source line.
//!
//! The map works on *segments*: the line is split before every character that NFC cannot
//! combine with what precedes it (a starter with `NFC_Quick_Check=Yes`), so each segment
//! normalizes on its own. A segment of one source character that becomes one form character of
//! the same UTF-16 length is *plain*; runs of plain segments are mapped by counting form
//! characters from an anchor. Any other segment is *opaque*: its own anchor, indivisible.

use std::iter;

use unicode_normalization::char::canonical_combining_class;
use unicode_normalization::{IsNormalized, UnicodeNormalization, is_nfc_quick};

/// Version of the normalization; part of the index version id.
pub const NORMALIZATION_VERSION: u32 = 1;

/// Longest stretch of form bytes mapped by counting characters: bounds [`SearchForm::utf16_at`]
/// to a binary search plus a scan of at most this many bytes (and one character).
const CHECKPOINT: usize = 64;

/// Search form of a line with a map from its byte offsets to UTF-16 offsets of the source.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct SearchForm {
    /// NFC, `ё`/`Ё` replaced with `е`/`Е`, full Unicode lower case.
    pub text: String,
    /// Starts of segment runs and opaque segments, ordered by `form`; the first is at 0. Each
    /// covers the form up to the next one or to the end.
    anchors: Vec<Anchor>,
    /// UTF-16 length of the source line.
    source_len: u32,
}

/// A point where the form and the source are aligned.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
struct Anchor {
    /// Byte offset in the form.
    form: u32,
    /// UTF-16 offset in the source.
    source: u32,
    /// The anchor starts one indivisible segment rather than a run of plain segments.
    opaque: bool,
}

/// Builds the search form of `line` and its offset map.
#[must_use]
pub fn search_form(line: &str) -> SearchForm {
    // NFC with `ё` replaced, before lower case: lower case runs once over the whole line, as
    // the final sigma depends on its neighbours.
    let mut folded = String::with_capacity(line.len());
    let mut anchors: Vec<Anchor> = Vec::new();
    let mut form_len = 0;
    let mut source_len = 0;
    for segment in segments(line) {
        let before = folded.len();
        folded.extend(segment.nfc().map(replace_yo));
        let out = &folded[before..];
        let lower_len: usize = out
            .chars()
            .flat_map(char::to_lowercase)
            .map(char::len_utf8)
            .sum();
        let segment_len = segment.encode_utf16().count();

        if is_plain(segment, out, segment_len) {
            let in_run = anchors
                .last()
                .is_some_and(|a| !a.opaque && form_len - (a.form as usize) < CHECKPOINT);
            if !in_run {
                anchors.push(Anchor {
                    form: saturate(form_len),
                    source: saturate(source_len),
                    opaque: false,
                });
            }
        } else {
            anchors.push(Anchor {
                form: saturate(form_len),
                source: saturate(source_len),
                opaque: true,
            });
        }
        form_len += lower_len;
        source_len += segment_len;
    }
    let text = folded.to_lowercase();
    // Lower case maps character by character; only the choice between `σ` and `ς` depends on
    // the context, and both have the same length.
    debug_assert_eq!(text.len(), form_len);
    SearchForm {
        text,
        anchors,
        source_len: saturate(source_len),
    }
}

impl SearchForm {
    /// UTF-16 offset in the source of the form position `form_byte`, as the start of a range.
    ///
    /// Exact at segment boundaries, including 0 and `text.len()`. A position inside a source
    /// character's form (inside `i̇` from `İ`, or inside a UTF-8 sequence) rounds down to the
    /// start of that character; positions past the end give the source length.
    #[must_use]
    pub fn utf16_at(&self, form_byte: usize) -> u32 {
        self.map(form_byte, false)
    }

    /// UTF-16 offset in the source of the form position `form_byte`, as the end of a range: like
    /// [`Self::utf16_at`], but a position inside a source character's form rounds up to its end,
    /// so a highlight never splits a source character.
    #[must_use]
    pub fn utf16_end_at(&self, form_byte: usize) -> u32 {
        self.map(form_byte, true)
    }

    fn map(&self, form_byte: usize, round_up: bool) -> u32 {
        if form_byte >= self.text.len() {
            return self.source_len;
        }
        let next = self
            .anchors
            .partition_point(|a| a.form as usize <= form_byte);
        let Some(anchor) = next.checked_sub(1).and_then(|i| self.anchors.get(i)) else {
            return 0;
        };
        let start = anchor.form as usize;
        if form_byte == start {
            return anchor.source;
        }
        if anchor.opaque {
            return if round_up {
                self.anchors.get(next).map_or(self.source_len, |a| a.source)
            } else {
                anchor.source
            };
        }
        // A run: one form character per source character, of the same UTF-16 length.
        let mut source = anchor.source;
        for (offset, c) in self.text.get(start..).unwrap_or_default().char_indices() {
            let at = start + offset;
            if at + c.len_utf8() <= form_byte || (round_up && at < form_byte) {
                source += saturate(c.len_utf16());
            }
            if at + c.len_utf8() >= form_byte {
                break;
            }
        }
        source
    }
}

/// Splits `line` into segments that NFC normalizes independently.
fn segments(line: &str) -> impl Iterator<Item = &str> {
    let mut rest = line;
    iter::from_fn(move || {
        let mut chars = rest.char_indices();
        chars.next()?;
        let end = chars
            .find(|&(_, c)| starts_segment(c))
            .map_or(rest.len(), |(i, _)| i);
        let (segment, tail) = rest.split_at(end);
        rest = tail;
        Some(segment)
    })
}

/// Whether NFC can never combine `c` with what precedes it: a starter that is not a possible
/// second part of a composition and has no decomposition starting with one.
fn starts_segment(c: char) -> bool {
    c.is_ascii()
        || (canonical_combining_class(c) == 0 && is_nfc_quick(iter::once(c)) == IsNormalized::Yes)
}

/// A segment of one character whose form is one character of the same UTF-16 length.
fn is_plain(segment: &str, out: &str, segment_len: usize) -> bool {
    let mut source = segment.chars();
    let mut folded = out.chars();
    let (Some(_), None, Some(c), None) =
        (source.next(), source.next(), folded.next(), folded.next())
    else {
        return false;
    };
    let mut lower = c.to_lowercase();
    matches!((lower.next(), lower.next()), (Some(l), None) if l.len_utf16() == segment_len)
}

fn replace_yo(c: char) -> char {
    match c {
        'ё' => 'е',
        'Ё' => 'Е',
        c => c,
    }
}

/// Offsets above `u32::MAX` do not occur: a frame is at most 16 MiB.
fn saturate(n: usize) -> u32 {
    u32::try_from(n).unwrap_or(u32::MAX)
}

#[cfg(test)]
mod tests {
    use std::iter;

    use unicode_normalization::char::canonical_combining_class;
    use unicode_normalization::{IsNormalized, UnicodeNormalization, is_nfc_quick};

    use super::{SearchForm, search_form, starts_segment};

    fn utf16_len(s: &str) -> u32 {
        u32::try_from(s.encode_utf16().count()).unwrap()
    }

    /// The form computed on the whole line at once, without the map.
    fn whole_line_form(s: &str) -> String {
        s.nfc()
            .map(|c| match c {
                'ё' => 'е',
                'Ё' => 'Е',
                c => c,
            })
            .collect::<String>()
            .to_lowercase()
    }

    /// Checks the invariants of the map on every byte of the form.
    fn check_map(src: &str, form: &SearchForm) {
        assert_eq!(form.text, whole_line_form(src), "form of {src:?}");
        assert_eq!(form.utf16_at(0), 0, "start of {src:?}");
        assert_eq!(
            form.utf16_at(form.text.len()),
            utf16_len(src),
            "end of {src:?}"
        );
        assert_eq!(
            form.utf16_end_at(form.text.len()),
            utf16_len(src),
            "end of {src:?}"
        );
        assert_eq!(
            form.utf16_at(form.text.len() + 7),
            utf16_len(src),
            "past the end"
        );
        let mut prev = 0;
        for b in 0..=form.text.len() {
            let (start, end) = (form.utf16_at(b), form.utf16_end_at(b));
            assert!(
                start <= end && start >= prev,
                "{src:?} at {b}: {start}..{end}"
            );
            prev = start;
        }
    }

    /// Splitting before `c` is safe when the first character of its decomposition is a starter
    /// that NFC never composes with a preceding character (UAX #15, §9).
    #[test]
    fn segment_starters_do_not_combine_backward() {
        let mut starters = 0;
        for c in (0..=u32::from(char::MAX))
            .filter_map(char::from_u32)
            .filter(|&c| starts_segment(c))
        {
            starters += 1;
            let first = c.nfd().next().expect("a decomposition is not empty");
            assert_eq!(canonical_combining_class(first), 0, "{c:?}");
            assert_eq!(is_nfc_quick(iter::once(first)), IsNormalized::Yes, "{c:?}");
        }
        assert!(starters > 1_000_000, "{starters}");
    }

    #[test]
    fn form_map_stays_small() {
        let line = "Строка с текстом и ascii words, 8192 символа. ".repeat(180);
        let form = search_form(&line);
        assert!(
            form.anchors.len() <= form.text.len() / 64 + 1,
            "{}",
            form.anchors.len()
        );
    }

    #[test]
    fn form_nfd_and_yo() {
        let src = "Е\u{0308}жик и\u{0306}од";
        let form = search_form(src);
        assert_eq!(form.text, "ежик йод");
        let hedgehog = form.text.find("ежик").expect("ежик in the form");
        let iodine = form.text.find("йод").expect("йод in the form");
        assert_eq!(form.utf16_at(hedgehog), 0);
        assert_eq!(form.utf16_end_at(hedgehog + "ежик".len()), 5);
        assert_eq!(form.utf16_at(iodine), 6);
        assert_eq!(form.utf16_at(form.text.len()), 10);
        check_map(src, &form);
    }

    #[test]
    fn form_keeps_other_diacritics() {
        assert_eq!(search_form("ќ К\u{0301}").text, "ќ ќ");
    }

    #[test]
    fn form_offsets_surrogates() {
        let src = "😀 needle";
        let form = search_form(src);
        let at = form.text.find("needle").expect("needle in the form");
        assert_eq!(form.utf16_at(at), 3);
        // Inside the emoji: the start rounds down, the end up.
        assert_eq!(form.utf16_at(2), 0);
        assert_eq!(form.utf16_end_at(2), 2);
        check_map(src, &form);
    }

    #[test]
    fn form_lowercase_changes_length() {
        let src = "İx";
        let form = search_form(src);
        assert_eq!(form.text, "i\u{0307}x");
        assert_eq!(
            form.utf16_at(form.text.find('x').expect("x in the form")),
            1
        );
        // `i` alone is part of `İ`: a range ending there covers the whole source character.
        assert_eq!(form.utf16_at(1), 0);
        assert_eq!(form.utf16_end_at(1), 1);
        check_map(src, &form);
    }

    #[test]
    fn form_matches_whole_line_normalization() {
        let long = "Длинная строка, ASCII и 😀 эмодзи; ".repeat(40);
        let cases = [
            "",
            "plain ascii",
            "ΟΔΟΣ ΟΔΟΣΑ Σ",                  // final sigma depends on neighbours
            "a\u{0301}\u{0323}b",            // marks reordered, then composed
            "\u{212A}elvin \u{212B}ngstrom", // singletons decompose and compose
            "\u{1100}\u{1161}\u{11A8} \u{AC00}\u{11A8}", // Hangul jamo compose across starters
            "\u{0B47}\u{0B3E}",              // two starters compose (Oriya)
            "\u{0301}leading mark",
            "Ё\u{0308}Ё ё", // ё with an extra diaeresis
            "ǅ ﬃ ẞ",        // title case, ligature, capital sharp s
            long.as_str(),
        ];
        for src in cases {
            check_map(src, &search_form(src));
        }
    }
}
