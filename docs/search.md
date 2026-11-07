**English** | [Русский](search.ru.md)

# Search

How to find a note and a line in it: words, phrases, fragments of code and search by meaning, and
what the status line under the hits says. For users of Smartnote 2.1.0 and later.

The search is local. It runs in the separate process `smartnote-search` on the computer, the
server takes no part in it, and the text of the notes and the queries never leave the device. It
searches the names and texts of the current notes, not their history.

## Searching

- The field `Search records` is above the editor, right of **New** and **Remove**; `Ctrl+F`
  (`Cmd+F` on macOS) puts the focus into it.
- The hits appear under the field about 150 ms after typing, at most 10 of them, at most 3 lines of
  one note. A hit shows the note name, `name` or `line N` (the lines of the text count from 1) and
  the line with every match highlighted.
- `↑`/`↓` choose a hit, `Enter` or a click opens it, `Esc` closes the hits and returns the focus
  where it was.
- While the answer to a changed query is pending, the hits of the previous query stay dimmed and do
  not open; `Enter` then searches the query at once and opens the chosen hit of the new answer.

## Opening a hit

The note opens and the match is selected: in the text, which scrolls to it, or in the Name field
for a `name` hit.

- If the editor holds unsaved changes, opening any hit, even one in the same note, asks
  `Discard unsaved changes in the editor?` with **Discard** and **Cancel**; **Cancel** has the focus, so `Enter`
  keeps the draft. Choosing another note in the list, **New**, **Restore** and closing the window ask
  the same, unlike v1.
- If the text of the note changed after the search, Smartnote searches again and selects the match
  of the same note nearest to the old line; when there is none, the note opens at the top without a
  selection.

## Query syntax

| Query | What is found |
|---|---|
| `встреча клиент` | words in any order, with other word forms (Russian and English morphology): «Встречи с клиентами» |
| `"quarterly sales report"` | the phrase in double quotes, words in this order |
| `ryBack` | the query as a substring, also inside a word: `retryBackoff` |
| `<< attempt`, `==`, `(` | fragments of code with operators and brackets |

- There are no operators; characters of code (`:`, `(`, `-`, `*` and others) do not break the
  query. A query is cut to 256 characters.
- The whole query is also searched as one continuous substring when it has at least 3 characters
  or contains a character that is not a letter, a digit or a space. A query of 1–2 letters or
  digits (`ok`, `ip`) finds whole words only.
- If the whole query is one phrase in quotes, the substring search takes it without the quotes.

## Semantic search

Smartnote also finds lines by meaning, in Russian and in English and between the two languages,
with a model built into the application (`multilingual-e5-small`). It takes queries of 3 characters
or more.

- A line found only by meaning is shown when it is close enough to the query, at most 3 such lines
  per answer. It has no highlight; `Enter` selects its whole fragment.
- The threshold is strict: most queries in a language other than that of the note find nothing by
  meaning and show `No matches`. Words and substrings remain the main way to search.
- Semantic search needs AVX2 on x86-64 CPUs and, on Windows, the Visual C++ Redistributable
  ([Installation](install.md#semantic-search-requirements)). Without them words, phrases and
  substrings still work.
- After an unlock the model loads and the notes get their vectors in the background; until then
  words, phrases and substrings already work.

## Status line

One line under the hits says what limits the answer:

| Line | Meaning |
|---|---|
| `Search unavailable` | the search process does not run or did not answer; Smartnote restarts it and repeats the query when it is back |
| `No matches` | nothing found for the query |
| `Semantic search is loading` | the model is loading; words, phrases and substrings already work |
| `Indexing N of M` | N of M notes are ready for semantic search |
| `Semantic search unavailable` | no search by meaning: the model files are missing or damaged, the CPU has no AVX2 or the ONNX Runtime library did not load; `smartnote.log` has the reason |

If the search process keeps failing, Smartnote stops restarting it and the search stays
`Search unavailable` until the next start of Smartnote. The notes, the list and saving keep working
in every case: a save never waits for the search.
