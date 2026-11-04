//! Word search: a tantivy RAM index with one document per line (spec §6.5).

use std::collections::HashMap;

use tantivy::collector::TopDocs;
use tantivy::query::{BooleanQuery, Occur, PhraseQuery, Query as TantivyQuery, TermQuery};
use tantivy::schema::{
    FAST, Field, INDEXED, IndexRecordOption, Schema, TextFieldIndexing, TextOptions,
};
use tantivy::tokenizer::{
    Language, RemoveLongFilter, SimpleTokenizer, Stemmer, TextAnalyzer, Token,
};
use tantivy::{DocAddress, Index, IndexReader, IndexWriter, ReloadPolicy, Term, doc};

use super::{LineHit, NoteKey};
use crate::text::Query;

/// Name of the analyzer of the `words` field.
const ANALYZER: &str = "words";
/// Longest word kept by the index, in bytes.
const MAX_WORD: usize = 64;
/// Documents examined per search (spec §6.8).
const MAX_DOCS: usize = 1000;
/// Memory of the writer; the minimum tantivy accepts per thread is 15 MB.
const WRITER_MEMORY: usize = 15_000_000;

/// Word index of the lines of notes: one tantivy document per line, text not stored.
pub struct LexicalIndex {
    index: Index,
    writer: IndexWriter,
    reader: IndexReader,
    note: Field,
    line: Field,
    words: Field,
}

impl LexicalIndex {
    /// Creates an empty index.
    ///
    /// # Errors
    ///
    /// Returns the tantivy error.
    pub fn new() -> tantivy::Result<Self> {
        let mut schema = Schema::builder();
        let note = schema.add_u64_field("note", INDEXED | FAST);
        let line = schema.add_u64_field("line", FAST);
        let indexing = TextFieldIndexing::default()
            .set_tokenizer(ANALYZER)
            .set_index_option(IndexRecordOption::WithFreqsAndPositions);
        let words = schema.add_text_field(
            "words",
            TextOptions::default().set_indexing_options(indexing),
        );
        let index = Index::create_in_ram(schema.build());
        index.tokenizers().register(
            ANALYZER,
            TextAnalyzer::builder(SimpleTokenizer::default())
                .filter(RemoveLongFilter::limit(MAX_WORD))
                .filter(Stemmer::new(Language::Russian))
                .filter(Stemmer::new(Language::English))
                .build(),
        );
        let writer = index.writer_with_num_threads(1, WRITER_MEMORY)?;
        let reader = index
            .reader_builder()
            .reload_policy(ReloadPolicy::Manual)
            .try_into()?;
        Ok(Self {
            index,
            writer,
            reader,
            note,
            line,
            words,
        })
    }

    /// Replaces the lines of `note` with `forms` (search forms; line 0 is the title). Takes
    /// effect at the next [`commit`](Self::commit).
    ///
    /// # Errors
    ///
    /// Returns the tantivy error.
    pub fn replace_note(&mut self, note: NoteKey, forms: &[&str]) -> tantivy::Result<()> {
        self.remove_note(note)?;
        for (line, form) in (0_u64..).zip(forms) {
            self.writer
                .add_document(doc!(self.note => note, self.line => line, self.words => *form))?;
        }
        Ok(())
    }

    /// Removes the lines of `note`. Takes effect at the next [`commit`](Self::commit).
    ///
    /// # Errors
    ///
    /// Returns the tantivy error.
    pub fn remove_note(&mut self, note: NoteKey) -> tantivy::Result<()> {
        self.writer
            .delete_term(Term::from_field_u64(self.note, note));
        Ok(())
    }

    /// Removes all lines. Takes effect at the next [`commit`](Self::commit).
    ///
    /// # Errors
    ///
    /// Returns the tantivy error.
    pub fn clear(&mut self) -> tantivy::Result<()> {
        self.writer.delete_all_documents()?;
        Ok(())
    }

    /// Makes the changes visible to [`search`](Self::search).
    ///
    /// # Errors
    ///
    /// Returns the tantivy error.
    pub fn commit(&mut self) -> tantivy::Result<()> {
        self.writer.commit()?;
        self.reader.reload()
    }

    /// Searches words and phrases of `q` (any of them matches; BM25) and returns up to `k`
    /// lines with at most `per_note` per note, the cap applied while walking the best
    /// [`MAX_DOCS`] documents in rank order.
    ///
    /// `first` of the hits is 0: the index does not hold the text. Callers that have the line
    /// get the offset from [`first_match`](Self::first_match).
    ///
    /// # Errors
    ///
    /// Returns the tantivy error.
    pub fn search(&self, q: &Query, per_note: usize, k: usize) -> tantivy::Result<Vec<LineHit>> {
        let mut clauses: Vec<(Occur, Box<dyn TantivyQuery>)> = Vec::new();
        for word in &q.words {
            if let Some(clause) = self.phrase_clause(word) {
                clauses.push((Occur::Should, clause));
            }
        }
        for phrase in &q.phrases {
            if let Some(clause) = self.phrase_clause(phrase) {
                clauses.push((Occur::Should, clause));
            }
        }
        if clauses.is_empty() || per_note == 0 || k == 0 {
            return Ok(Vec::new());
        }

        let searcher = self.reader.searcher();
        let top = searcher.search(
            &BooleanQuery::new(clauses),
            &TopDocs::with_limit(MAX_DOCS).order_by_score(),
        )?;
        let mut per_note_taken: HashMap<NoteKey, usize> = HashMap::new();
        let mut hits = Vec::new();
        for (
            score,
            DocAddress {
                segment_ord,
                doc_id,
            },
        ) in top
        {
            if hits.len() >= k {
                break;
            }
            let fast = searcher.segment_reader(segment_ord).fast_fields();
            let (Some(note), Some(line)) = (
                fast.u64("note")?.first(doc_id),
                fast.u64("line")?.first(doc_id),
            ) else {
                continue;
            };
            let taken = per_note_taken.entry(note).or_default();
            if *taken < per_note {
                *taken += 1;
                hits.push(LineHit {
                    note,
                    line: u32::try_from(line).unwrap_or(u32::MAX),
                    score,
                    first: 0,
                });
            }
        }
        Ok(hits)
    }

    /// Byte offset in `form` of the first match of `q`: the first token whose analyzed term is
    /// a term of a word of `q`, or the start of a phrase of `q` (consecutive tokens).
    #[must_use]
    pub fn first_match(&self, form: &str, q: &Query) -> Option<usize> {
        let tokens = self.analyze(form);
        let words: Vec<String> = q
            .words
            .iter()
            .flat_map(|w| self.analyze(w))
            .map(|t| t.text)
            .collect();
        let by_word = tokens
            .iter()
            .find(|t| words.contains(&t.text))
            .map(|t| t.offset_from);
        let by_phrase = q.phrases.iter().filter_map(|p| {
            let terms: Vec<String> = self.analyze(p).into_iter().map(|t| t.text).collect();
            tokens
                .windows(terms.len().max(1))
                .find(|w| !terms.is_empty() && w.iter().map(|t| &t.text).eq(terms.iter()))
                .map(|w| w[0].offset_from)
        });
        by_word.into_iter().chain(by_phrase).min()
    }

    /// Query for a word or a phrase: a term for one token, a phrase for several; none when the
    /// analyzer drops all (an over-long word).
    fn phrase_clause(&self, text: &str) -> Option<Box<dyn TantivyQuery>> {
        let mut terms: Vec<Term> = self
            .analyze(text)
            .into_iter()
            .map(|t| Term::from_field_text(self.words, &t.text))
            .collect();
        match terms.len() {
            0 => None,
            1 => Some(Box::new(TermQuery::new(
                terms.remove(0),
                IndexRecordOption::WithFreqs,
            ))),
            _ => Some(Box::new(PhraseQuery::new(terms))),
        }
    }

    fn analyze(&self, text: &str) -> Vec<Token> {
        let Some(mut analyzer) = self.index.tokenizers().get(ANALYZER) else {
            return Vec::new();
        };
        let mut tokens = Vec::new();
        analyzer
            .token_stream(text)
            .process(&mut |token| tokens.push(token.clone()));
        tokens
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::text::{parse_query, search_form};

    fn index(notes: &[(NoteKey, &[&str])]) -> LexicalIndex {
        let mut index = LexicalIndex::new().expect("index");
        for &(note, lines) in notes {
            let forms: Vec<String> = lines.iter().map(|l| search_form(l).text).collect();
            let refs: Vec<&str> = forms.iter().map(String::as_str).collect();
            index.replace_note(note, &refs).expect("replace");
        }
        index.commit().expect("commit");
        index
    }

    fn top(index: &LexicalIndex, query: &str) -> Option<(NoteKey, u32)> {
        let hits = index.search(&parse_query(query), 3, 50).expect("search");
        hits.first().map(|h| (h.note, h.line))
    }

    #[test]
    fn words_morphology_ru_en() {
        let index = index(&[(
            1,
            &[
                "Ключ лежит в менеджере паролей",
                "2 eggs, 300 ml milk",
                "mix and fry on a hot pan",
            ],
        )]);
        assert_eq!(top(&index, "пароли"), Some((1, 0)));
        assert_eq!(top(&index, "менеджер пароля"), Some((1, 0)));
        assert_eq!(top(&index, "egg"), Some((1, 1)));
        assert_eq!(top(&index, "frying"), Some((1, 2)));
    }

    #[test]
    fn words_phrase() {
        let index = index(&[(
            1,
            &["семантический поиск работает", "поиск бывает семантический"],
        )]);
        let hits = index
            .search(&parse_query("\"семантический поиск\""), 3, 50)
            .expect("search");
        let lines: Vec<_> = hits.iter().map(|h| h.line).collect();
        assert_eq!(lines, [0]);
    }

    #[test]
    fn words_cap_per_note() {
        let many = vec!["пароль"; 10];
        let index = index(&[(1, &many), (2, &["пароль"]), (3, &["пароль"])]);
        let hits = index.search(&parse_query("пароль"), 3, 50).expect("search");
        let count = |n| hits.iter().filter(|h| h.note == n).count();
        assert_eq!((count(1), count(2), count(3)), (3, 1, 1));
    }

    #[test]
    fn words_replace_remove_clear_and_empty() {
        let mut index = index(&[(1, &["alpha"]), (2, &["alpha"])]);
        assert_eq!(
            index.search(&parse_query("alpha"), 3, 50).expect("s").len(),
            2
        );
        index.replace_note(1, &["beta"]).expect("replace");
        index.remove_note(2).expect("remove");
        index.commit().expect("commit");
        assert_eq!(
            index.search(&parse_query("alpha"), 3, 50).expect("s").len(),
            0
        );
        assert_eq!(top(&index, "beta"), Some((1, 0)));
        index.clear().expect("clear");
        index.commit().expect("commit");
        assert_eq!(
            index.search(&parse_query("beta"), 3, 50).expect("s").len(),
            0
        );
        assert_eq!(index.search(&parse_query(""), 3, 50).expect("s").len(), 0);
        assert_eq!(
            index.search(&parse_query("beta"), 0, 0).expect("s").len(),
            0
        );
    }

    #[test]
    fn words_first_match_offset() {
        let index = index(&[]);
        let form = "ключ лежит в менеджере паролей";
        let at = |q: &str| index.first_match(form, &parse_query(q));
        assert_eq!(at("пароли"), Some(form.find("паролей").expect("in form")));
        assert_eq!(
            at("\"лежит в\""),
            Some(form.find("лежит").expect("in form"))
        );
        assert_eq!(at("zzz"), None);
    }
}
