//! Parsing of a search query (spec §6.4, §6.6, §6.7).

use unicode_normalization::UnicodeNormalization;

use super::form::search_form;

/// Longest query in UTF-16 code units; the rest is dropped.
const MAX_QUERY_UTF16: usize = 256;

/// Shortest query, in characters, that the substring and the semantic search take regardless
/// of its characters.
const MIN_CHARS: usize = 3;

/// A query split for the kinds of search.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Query {
    /// Words outside quotes, in the search form: maximal runs of alphanumeric characters, as
    /// the word index tokenizes (`foo(bar)` gives `foo` and `bar`, `->` gives none).
    pub words: Vec<String>,
    /// Phrases in double quotes with at least one alphanumeric character, in the search form.
    /// An unclosed quote runs to the end of the query.
    pub phrases: Vec<String>,
    /// Needle of the substring search, in the search form: the whole query, or the content of
    /// the quotes when the whole query is one phrase.
    pub substring: Option<String>,
    /// Text for the semantic search: NFC of the query without quotes.
    pub semantic: Option<String>,
}

/// Parses a raw query: truncates it to 256 UTF-16 units (never inside a character), trims it
/// and splits it for the kinds of search.
///
/// The substring search runs when the needle has at least 3 characters or contains a
/// character that is neither alphanumeric nor whitespace (`==`, `->`, `(`); a quote counts only
/// when it stays in the needle. The semantic search runs from 3 characters.
#[must_use]
pub fn parse_query(raw: &str) -> Query {
    let query = truncate_utf16(raw, MAX_QUERY_UTF16).trim();

    let mut words = Vec::new();
    let mut phrases = Vec::new();
    // Even pieces are outside quotes, odd ones inside.
    for (i, piece) in query.split('"').enumerate() {
        let form = search_form(piece.trim()).text;
        if i % 2 == 1 {
            if form.chars().any(char::is_alphanumeric) {
                phrases.push(form);
            }
        } else {
            let tokens = form
                .split(|c: char| !c.is_alphanumeric())
                .filter(|w| !w.is_empty());
            words.extend(tokens.map(str::to_owned));
        }
    }

    let needle = search_form(single_phrase(query).unwrap_or(query).trim()).text;
    let substring = (needle.chars().count() >= MIN_CHARS
        || needle
            .chars()
            .any(|c| !c.is_alphanumeric() && !c.is_whitespace()))
    .then_some(needle);

    let unquoted: String = query.chars().filter(|&c| c != '"').nfc().collect();
    let unquoted = unquoted.trim();
    let semantic = (unquoted.chars().count() >= MIN_CHARS).then(|| unquoted.to_owned());

    Query {
        words,
        phrases,
        substring,
        semantic,
    }
}

/// The content of the quotes when the whole query is one phrase (closed or not).
fn single_phrase(query: &str) -> Option<&str> {
    let rest = query.strip_prefix('"')?;
    let inner = rest.strip_suffix('"').unwrap_or(rest);
    (!inner.contains('"')).then_some(inner)
}

/// The longest prefix of `s` of at most `max` UTF-16 code units.
fn truncate_utf16(s: &str, max: usize) -> &str {
    let mut units = 0;
    for (at, c) in s.char_indices() {
        units += c.len_utf16();
        if units > max {
            return &s[..at];
        }
    }
    s
}

#[cfg(test)]
mod tests {
    use super::{Query, parse_query};

    fn strings(items: &[&str]) -> Vec<String> {
        items.iter().map(ToString::to_string).collect()
    }

    #[test]
    fn parse_query_phrases_and_code() {
        let q = parse_query("\"retry backoff\" ->");
        assert_eq!(q.phrases, strings(&["retry backoff"]));
        assert!(q.words.is_empty(), "{q:?}");
        assert_eq!(q.substring.as_deref(), Some("\"retry backoff\" ->"));
        assert_eq!(q.semantic.as_deref(), Some("retry backoff ->"));

        assert_eq!(parse_query("\"a b\"").substring.as_deref(), Some("a b"));
    }

    #[test]
    fn parse_query_words_in_search_form() {
        let q = parse_query("  Ёлка  \"Новый Год\" foo(bar) ");
        assert_eq!(q.words, strings(&["елка", "foo", "bar"]));
        assert_eq!(q.phrases, strings(&["новый год"]));
        assert_eq!(q.substring.as_deref(), Some("елка  \"новый год\" foo(bar)"));
        assert_eq!(q.semantic.as_deref(), Some("Ёлка  Новый Год foo(bar)"));
    }

    #[test]
    fn parse_query_nfd_input() {
        let q = parse_query("и\u{0306}од");
        assert_eq!(q.words, strings(&["йод"]));
        assert_eq!(q.substring.as_deref(), Some("йод"));
        assert_eq!(q.semantic.as_deref(), Some("йод"));
    }

    #[test]
    fn parse_query_unclosed_quote() {
        let q = parse_query("\"retry back");
        assert_eq!(q.phrases, strings(&["retry back"]));
        assert!(q.words.is_empty(), "{q:?}");
        assert_eq!(q.substring.as_deref(), Some("retry back"));
        assert_eq!(q.semantic.as_deref(), Some("retry back"));
    }

    #[test]
    fn parse_query_short() {
        let ok = parse_query("ok");
        assert_eq!(ok.words, strings(&["ok"]));
        assert_eq!(ok.substring, None);
        assert_eq!(ok.semantic, None);
        assert_eq!(parse_query("==").substring.as_deref(), Some("=="));
        assert_eq!(parse_query("(").substring.as_deref(), Some("("));
        assert_eq!(
            parse_query("\"ok\"").substring,
            None,
            "the quotes are not part of the needle"
        );
        assert_eq!(parse_query("abc").substring.as_deref(), Some("abc"));
    }

    #[test]
    fn parse_query_degenerate() {
        for raw in ["", "   ", "\"", "\"\"", "\" \""] {
            assert_eq!(parse_query(raw), Query::default(), "{raw:?}");
        }
    }

    #[test]
    fn parse_query_truncates() {
        let raw = "а".repeat(300);
        let q = parse_query(&raw);
        let semantic = q.semantic.expect("semantic text");
        assert_eq!(semantic.encode_utf16().count(), 256);

        // A surrogate pair is not split: 255 units plus a pair would be 257.
        let raw = format!("{}😀tail", "a".repeat(255));
        let semantic = parse_query(&raw).semantic.expect("semantic text");
        assert_eq!(semantic, "a".repeat(255));
    }
}
