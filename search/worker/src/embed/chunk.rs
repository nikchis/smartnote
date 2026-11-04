//! Chunks of a note for embeddings (spec §6.7).

use std::iter;

/// Parameters of chunking, in tokens of the model; part of the index version id.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct ChunkParams {
    /// Desired size of a chunk with its prefix and special tokens.
    pub target: usize,
    /// Upper limit of a chunk with its prefix and special tokens (the model's input length).
    pub max: usize,
    /// A chunk smaller than this joins a neighbour.
    pub min_tail: usize,
    /// Lines shared by adjacent chunks of a paragraph split by lines.
    pub overlap_lines: usize,
    /// Size of a window over a line longer than the budget, with the prefix and special tokens.
    pub window: usize,
    /// Tokens shared by adjacent windows of a line.
    pub window_overlap: usize,
}

impl Default for ChunkParams {
    fn default() -> Self {
        Self {
            target: 256,
            max: 512,
            min_tail: 64,
            overlap_lines: 1,
            window: 256,
            window_overlap: 32,
        }
    }
}

/// A chunk: lines `first..=last` of `info` (numbered from 1), or a window of line `first`.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct ChunkSpan {
    /// First line.
    pub first: u32,
    /// Last line; equals `first` for a window.
    pub last: u32,
    /// Byte range of line `first` when the line is cut into windows; on char boundaries.
    pub window: Option<(usize, usize)>,
}

/// Chunks of a note.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Chunks {
    /// Length in bytes of the start of the name that the prefix of every chunk carries.
    pub name_len: usize,
    /// The chunks in order.
    pub spans: Vec<ChunkSpan>,
}

/// Splits the lines of `info` into chunks.
///
/// `count` gives the number of tokens of a text without special tokens. The budget of a chunk
/// includes the prefix `passage: <name>\n` and two special tokens. A name whose prefix would
/// take more than half of `max` (some characters expand to many tokens) is cut on a char
/// boundary until it takes no more; [`Chunks::name_len`] tells the part kept, which the
/// embedding of every chunk must use. Every chunk, prefix included, then fits `max`, so the
/// model input is never cut off. Paragraphs (runs of
/// non-empty lines) are packed whole up to `target`, so boundaries fall on empty lines; a
/// paragraph over `target` is split by lines with `overlap_lines` shared lines; a line over
/// `max` is cut into windows of `window` tokens sharing `window_overlap` tokens. A chunk under
/// `min_tail` tokens joins a neighbour when the union stays within `max`. Chunks never start
/// or end on an empty line; a note without non-empty lines has no chunks.
#[must_use]
pub fn chunk_note(
    name: &str,
    lines: &[&str],
    count: &dyn Fn(&str) -> usize,
    p: &ChunkParams,
) -> Chunks {
    // sums[i] = tokens of lines 0..i, an empty line counting 0.
    let sums = iter::once(0)
        .chain(lines.iter().scan(0, |total, line| {
            *total += if is_blank(line) { 0 } else { count(line) };
            Some(*total)
        }))
        .collect();
    let name_len = prefix_name_len(name, count, p);
    let chunker = Chunker {
        lines,
        sums,
        prefix: prefix_tokens(&name[..name_len], count),
        count,
        p,
    };
    Chunks {
        name_len,
        spans: chunker.run(),
    }
}

/// Tokens of the prefix of a chunk with `name`, special tokens included.
fn prefix_tokens(name: &str, count: &dyn Fn(&str) -> usize) -> usize {
    count(&format!("passage: {name}\n")) + SPECIAL_TOKENS
}

/// Length in bytes of the longest start of `name`, on a char boundary, whose prefix takes at
/// most half of `max`: the whole name as a rule.
fn prefix_name_len(name: &str, count: &dyn Fn(&str) -> usize, p: &ChunkParams) -> usize {
    let fits = |end: usize| prefix_tokens(&name[..end], count) <= p.max / 2;
    if fits(name.len()) {
        return name.len();
    }
    let bounds: Vec<usize> = name.char_indices().map(|(at, _)| at).collect();
    // bounds[0] is 0: the empty name always fits.
    bounds[last_fitting(0, bounds.len() - 1, |i| fits(bounds[i]))]
}

/// `<s>` and `</s>` around every input of the model.
const SPECIAL_TOKENS: usize = 2;

fn is_blank(line: &str) -> bool {
    line.trim().is_empty()
}

/// A chunk under construction: lines `first..=last` (indexes from 0) with `tokens` tokens
/// without the prefix, or a window of line `first`.
#[derive(Clone, Copy, Debug)]
struct Piece {
    first: usize,
    last: usize,
    tokens: usize,
    window: Option<(usize, usize)>,
}

struct Chunker<'a> {
    lines: &'a [&'a str],
    /// Running totals of line tokens: `sums[i]` for lines `0..i`.
    sums: Vec<usize>,
    /// Tokens of the prefix with the special tokens.
    prefix: usize,
    count: &'a dyn Fn(&str) -> usize,
    p: &'a ChunkParams,
}

impl Chunker<'_> {
    fn run(&self) -> Vec<ChunkSpan> {
        let mut pieces = Vec::new();
        let mut current: Option<Piece> = None;
        for (first, last) in self.paragraphs() {
            let tokens = self.tokens(first, last);
            let has_long_line = (first..=last).any(|i| self.is_long(i));
            if has_long_line || self.prefix + tokens > self.p.target {
                pieces.extend(current.take());
                self.split_paragraph(first, last, &mut pieces);
                continue;
            }
            current = match current {
                Some(c) if self.prefix + c.tokens + tokens <= self.p.target => Some(Piece {
                    last,
                    tokens: c.tokens + tokens,
                    ..c
                }),
                other => {
                    pieces.extend(other);
                    Some(Piece {
                        first,
                        last,
                        tokens,
                        window: None,
                    })
                }
            };
        }
        pieces.extend(current);
        self.join_small(&mut pieces);
        debug_assert!(
            pieces.iter().all(|c| self.prefix + c.tokens <= self.p.max),
            "a chunk over the model input"
        );
        pieces.iter().map(span).collect()
    }

    /// Runs of non-empty lines as `(first, last)` indexes.
    fn paragraphs(&self) -> Vec<(usize, usize)> {
        let mut runs = Vec::new();
        let mut start = None;
        for (i, line) in self.lines.iter().enumerate() {
            match (start, is_blank(line)) {
                (None, false) => start = Some(i),
                (Some(s), true) => {
                    runs.push((s, i - 1));
                    start = None;
                }
                _ => {}
            }
        }
        if let Some(s) = start {
            runs.push((s, self.lines.len() - 1));
        }
        runs
    }

    fn tokens(&self, first: usize, last: usize) -> usize {
        self.sums[last + 1] - self.sums[first]
    }

    /// The line does not fit a chunk on its own.
    fn is_long(&self, i: usize) -> bool {
        self.prefix + self.tokens(i, i) > self.p.max
    }

    /// Splits lines `first..=last` into chunks of whole lines up to `target` that share
    /// `overlap_lines` lines; long lines become windows, without overlap with their neighbours.
    fn split_paragraph(&self, first: usize, last: usize, pieces: &mut Vec<Piece>) {
        let mut start = first;
        // Lines before this one are already in a chunk: every chunk takes at least one new line.
        let mut fresh = first;
        while fresh <= last {
            if self.is_long(fresh) {
                self.split_line(fresh, pieces);
                fresh += 1;
                start = fresh;
                continue;
            }
            let mut end = fresh;
            while end < last
                && !self.is_long(end + 1)
                && self.prefix + self.tokens(start, end + 1) <= self.p.target
            {
                end += 1;
            }
            pieces.push(Piece {
                first: start,
                last: end,
                tokens: self.tokens(start, end),
                window: None,
            });
            fresh = end + 1;
            start = self.overlap_start(start, end, last);
        }
    }

    /// First line of the chunk after `start..=end`: `overlap_lines` lines back when the chunk
    /// has more lines than that and the next line fits with them; otherwise the next line.
    fn overlap_start(&self, start: usize, end: usize, last: usize) -> usize {
        let next = end + 1;
        let lines = end - start + 1;
        if next > last || lines <= self.p.overlap_lines || self.is_long(next) {
            return next;
        }
        let overlapped = next - self.p.overlap_lines;
        if self.prefix + self.tokens(overlapped, next) <= self.p.max {
            overlapped
        } else {
            next
        }
    }

    /// Cuts line `i` into windows of at most the window budget that share `window_overlap`
    /// tokens. The budget is what `window` leaves after the prefix, but at least half of
    /// `window`, and never more than `max` leaves: the prefix takes at most half of `max`.
    fn split_line(&self, i: usize, pieces: &mut Vec<Piece>) {
        let line = self.lines[i];
        // Char boundaries of the line, its end included.
        let bounds: Vec<usize> = line
            .char_indices()
            .map(|(at, _)| at)
            .chain([line.len()])
            .collect();
        let budget = self
            .p
            .window
            .saturating_sub(self.prefix)
            .max(self.p.window / 2)
            .min(self.p.max.saturating_sub(self.prefix))
            .max(1);
        let tokens = |from: usize, to: usize| (self.count)(&line[bounds[from]..bounds[to]]);
        let last = bounds.len() - 1;
        let mut start = 0;
        loop {
            // The window takes at least one char, then as many as fit the budget.
            let end = last_fitting(start + 1, last, |to| tokens(start, to) <= budget);
            pieces.push(Piece {
                first: i,
                last: i,
                tokens: tokens(start, end),
                window: Some((bounds[start], bounds[end])),
            });
            if end == last {
                return;
            }
            // The next window starts where the last `window_overlap` tokens of this one begin.
            let next = first_fitting(start + 1, end, |from| {
                tokens(from, end) <= self.p.window_overlap
            });
            start = next;
        }
    }

    /// Joins every chunk of whole lines under `min_tail` tokens with the previous chunk, or
    /// else the next one, when the union fits `max`.
    fn join_small(&self, pieces: &mut Vec<Piece>) {
        let mut i = 0;
        while i < pieces.len() {
            if self.is_small(&pieces[i]) {
                if i > 0
                    && let Some(joined) = self.join(&pieces[i - 1], &pieces[i])
                {
                    pieces[i - 1] = joined;
                    pieces.remove(i);
                    continue;
                }
                if i + 1 < pieces.len()
                    && let Some(joined) = self.join(&pieces[i], &pieces[i + 1])
                {
                    pieces[i] = joined;
                    pieces.remove(i + 1);
                    continue;
                }
            }
            i += 1;
        }
    }

    fn is_small(&self, piece: &Piece) -> bool {
        piece.window.is_none() && piece.tokens < self.p.min_tail
    }

    /// The union of two adjacent chunks of whole lines, if it fits `max`.
    fn join(&self, a: &Piece, b: &Piece) -> Option<Piece> {
        if a.window.is_some() || b.window.is_some() {
            return None;
        }
        let tokens = self.tokens(a.first, b.last);
        (self.prefix + tokens <= self.p.max).then_some(Piece {
            first: a.first,
            last: b.last,
            tokens,
            window: None,
        })
    }
}

/// The largest `x` in `lo..=hi` with `fits(x)`, assuming `fits` holds up to some point and not
/// after it; `lo` when nothing fits. Gallops from `lo`, so the cost follows the result, not `hi`.
fn last_fitting(lo: usize, hi: usize, fits: impl Fn(usize) -> bool) -> usize {
    let (mut good, mut step) = (lo, 1);
    let mut bad = loop {
        let probe = good + step;
        if probe > hi {
            break hi + 1;
        }
        if !fits(probe) {
            break probe;
        }
        good = probe;
        step *= 2;
    };
    while bad - good > 1 {
        let mid = good + (bad - good) / 2;
        if fits(mid) {
            good = mid;
        } else {
            bad = mid;
        }
    }
    good
}

/// The smallest `x` in `lo..=hi` with `fits(x)`, assuming `fits` fails up to some point and
/// holds after it; `hi` when only `hi` fits.
fn first_fitting(lo: usize, hi: usize, fits: impl Fn(usize) -> bool) -> usize {
    let (mut bad, mut good) = (lo, hi);
    if fits(lo) {
        return lo;
    }
    while good - bad > 1 {
        let mid = bad + (good - bad) / 2;
        if fits(mid) {
            good = mid;
        } else {
            bad = mid;
        }
    }
    good
}

fn span(piece: &Piece) -> ChunkSpan {
    let line = |i: usize| u32::try_from(i + 1).unwrap_or(u32::MAX);
    ChunkSpan {
        first: line(piece.first),
        last: line(piece.last),
        window: piece.window,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// One token per word: the prefix `passage: N\n` is 2 tokens, 4 with the special tokens.
    fn words(text: &str) -> usize {
        text.split_whitespace().count()
    }

    const PREFIX: usize = 4;

    /// A line of `n` words, numbered from `from`.
    fn line(n: usize, from: usize) -> String {
        (from..from + n)
            .map(|i| format!("w{i}"))
            .collect::<Vec<_>>()
            .join(" ")
    }

    /// Paragraphs of 10-word lines with the given token counts, separated by empty lines.
    fn paragraphs(tokens: &[usize]) -> Vec<String> {
        let mut lines = Vec::new();
        for (i, &n) in tokens.iter().enumerate() {
            if i > 0 {
                lines.push(String::new());
            }
            for start in (0..n).step_by(10) {
                lines.push(line(10.min(n - start), start));
            }
        }
        lines
    }

    fn chunk(lines: &[String]) -> Vec<ChunkSpan> {
        let lines: Vec<&str> = lines.iter().map(String::as_str).collect();
        chunk_note("N", &lines, &words, &ChunkParams::default()).spans
    }

    /// Tokens of a chunk with its prefix and special tokens.
    fn size(lines: &[String], span: &ChunkSpan) -> usize {
        let text = match span.window {
            Some((start, end)) => lines[span.first as usize - 1][start..end].to_owned(),
            None => lines[span.first as usize - 1..span.last as usize].join("\n"),
        };
        PREFIX + words(&text)
    }

    fn ranges(spans: &[ChunkSpan]) -> Vec<(u32, u32)> {
        spans.iter().map(|s| (s.first, s.last)).collect()
    }

    #[test]
    fn chunk_params_default() {
        let p = ChunkParams::default();

        assert_eq!(
            (
                p.target,
                p.max,
                p.min_tail,
                p.overlap_lines,
                p.window,
                p.window_overlap
            ),
            (256, 512, 64, 1, 256, 32)
        );
    }

    #[test]
    fn chunk_paragraph_boundary() {
        // Six paragraphs of 100 tokens (10 lines each) separated by empty lines 11, 22, ...
        let lines = paragraphs(&[100; 6]);

        let spans = chunk(&lines);

        assert_eq!(ranges(&spans), [(1, 21), (23, 43), (45, 65)]);
        for span in &spans {
            assert!(span.window.is_none());
            assert!(size(&lines, span) <= 512, "{span:?}");
            if span.first > 1 {
                assert!(lines[span.first as usize - 2].is_empty(), "{span:?}");
            }
            if (span.last as usize) < lines.len() {
                assert!(lines[span.last as usize].is_empty(), "{span:?}");
            }
        }
    }

    #[test]
    fn chunk_small_tail_joins() {
        // 4 + 120 + 120 = 244 fits the target; the 30-token tail joins that chunk.
        let lines = paragraphs(&[120, 120, 30]);

        let spans = chunk(&lines);

        assert_eq!(ranges(&spans), [(1, u32::try_from(lines.len()).unwrap())]);
        assert_eq!(size(&lines, &spans[0]), PREFIX + 270);
    }

    #[test]
    fn chunk_tail_of_min_size_stays() {
        let lines = paragraphs(&[120, 120, 70]);

        let spans = chunk(&lines);

        assert_eq!(ranges(&spans), [(1, 25), (27, 33)]);
    }

    #[test]
    fn chunk_large_paragraph_overlaps() {
        // 700 tokens in 70 lines of 10 words, no empty lines.
        let lines: Vec<String> = (0..70).map(|i| line(10, i * 10)).collect();

        let spans = chunk(&lines);

        assert!(spans.len() > 1, "{spans:?}");
        assert_eq!(spans.first().map(|s| s.first), Some(1));
        assert_eq!(spans.last().map(|s| s.last), Some(70));
        for pair in spans.windows(2) {
            assert_eq!(
                pair[1].first, pair[0].last,
                "exactly one shared line: {pair:?}"
            );
        }
        for span in &spans {
            assert!(span.window.is_none());
            assert!(size(&lines, span) <= 512, "{span:?}");
        }
    }

    #[test]
    fn chunk_single_long_line() {
        // One line of 2000 two-byte-letter words: windows must fall on char boundaries.
        let long: String = (0..2000)
            .map(|i| format!("слово{i}"))
            .collect::<Vec<_>>()
            .join(" ");
        let lines = vec![long.clone()];

        let spans = chunk(&lines);

        assert!(spans.len() > 1, "{spans:?}");
        let windows: Vec<(usize, usize)> = spans
            .iter()
            .map(|s| {
                assert_eq!((s.first, s.last), (1, 1));
                s.window.expect("a window")
            })
            .collect();
        assert_eq!(windows.first().map(|w| w.0), Some(0));
        assert_eq!(windows.last().map(|w| w.1), Some(long.len()));
        for &(start, end) in &windows {
            assert!(long.is_char_boundary(start) && long.is_char_boundary(end));
            assert!(start < end);
            assert!(words(&long[start..end]) + PREFIX <= 256, "{start}..{end}");
        }
        for pair in windows.windows(2) {
            let ((_, prev_end), (next_start, _)) = (pair[0], pair[1]);
            assert!(next_start < prev_end, "{pair:?}");
            assert_eq!(words(&long[next_start..prev_end]), 32, "{pair:?}");
        }
    }

    #[test]
    fn chunk_long_line_between_paragraphs() {
        let mut lines = paragraphs(&[100]);
        lines.push(String::new());
        lines.push(line(1000, 0));
        lines.push(String::new());
        lines.extend(paragraphs(&[100]));

        let spans = chunk(&lines);

        assert_eq!(
            spans.first().map(|s| (s.first, s.last, s.window)),
            Some((1, 10, None))
        );
        assert_eq!(
            spans.last().map(|s| (s.first, s.last, s.window)),
            Some((14, 23, None))
        );
        let middle = &spans[1..spans.len() - 1];
        assert!(middle.len() > 1);
        assert!(
            middle
                .iter()
                .all(|s| s.first == 12 && s.last == 12 && s.window.is_some())
        );
    }

    /// Like `words`, but U+FDFA counts 7 tokens, as it about does with the pinned tokenizer.
    fn expanding(text: &str) -> usize {
        words(text) + 6 * text.chars().filter(|&c| c == '\u{FDFA}').count()
    }

    /// From the review of PR 6: a valid name of 64 U+FDFA takes most of the model input with
    /// its prefix. The name is cut so that its prefix takes at most half of `max`, and every
    /// chunk, prefix and special tokens included, fits the model input: `Model::embed` would
    /// cut off the tail of a longer one.
    #[test]
    fn chunk_expanding_name_fits_model_input() {
        let name = "\u{FDFA}".repeat(64);
        let lines = [line(130, 0), line(300, 0)];
        let lines: Vec<&str> = lines.iter().map(String::as_str).collect();
        let p = ChunkParams::default();

        let chunks = chunk_note(&name, &lines, &expanding, &p);

        assert!(name.is_char_boundary(chunks.name_len));
        let prefix = expanding(&format!("passage: {}\n", &name[..chunks.name_len])) + 2;
        assert!(prefix <= p.max / 2, "prefix of {prefix} tokens");
        assert!(chunks.name_len > 0, "the name is cut, not dropped");
        for span in &chunks.spans {
            let first = span.first as usize - 1;
            let body = match span.window {
                Some((start, end)) => lines[first][start..end].to_owned(),
                None => lines[first..span.last as usize].join("\n"),
            };
            let input = prefix + expanding(&body);
            assert!(input <= p.max, "{span:?}: {input} tokens");
        }
        let last = chunks.spans.last().expect("chunks");
        assert_eq!(
            last.window.map(|w| w.1),
            Some(lines[1].len()),
            "the tail is covered"
        );
    }

    #[test]
    fn chunk_short_name_is_whole() {
        let lines = [line(10, 0)];
        let lines: Vec<&str> = lines.iter().map(String::as_str).collect();

        let chunks = chunk_note("Name", &lines, &words, &ChunkParams::default());

        assert_eq!(chunks.name_len, "Name".len());
    }

    #[test]
    fn chunk_empty_note() {
        let lines = vec![String::new(), "   ".to_owned()];

        assert_eq!(chunk(&lines), []);
    }
}
