import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SearchHitDTO, SearchStatusDTO } from "./api";
import { highlight, isFindShortcut, SearchBox, type SearchApi } from "./search.svelte";

function hit(noteId: string, line: number, text = "word"): SearchHitDTO {
  return { noteId, line, start: 0, end: text.length, text, snippet: text, marks: [[0, text.length]], sources: ["words"] };
}

/** FakeSearch answers each query with the hits of results, or with one of its pending promises. */
class FakeSearch implements SearchApi {
  calls: string[] = [];
  results: Record<string, SearchHitDTO[] | null> = {};
  fail = false;
  /** code is the UIError code of a failed search. */
  code = "internal";
  pending: { query: string; resolve: (hits: SearchHitDTO[]) => void }[] = [];
  hold = false;

  Search(q: string): Promise<SearchHitDTO[] | null> {
    this.calls.push(q);
    if (this.fail) return Promise.reject(new Error(JSON.stringify({ code: this.code, message: "search failed" })));
    if (this.hold) return new Promise((resolve) => this.pending.push({ query: q, resolve }));
    return Promise.resolve(this.results[q] ?? []);
  }
}

const ready: SearchStatusDTO = { notes: 500, queued: 0, lexical: "ready", semantic: "ready" };

let api: FakeSearch;
let box: SearchBox;

beforeEach(() => {
  vi.useFakeTimers();
  api = new FakeSearch();
  box = new SearchBox(api);
});

afterEach(() => {
  vi.useRealTimers();
});

describe("SearchBox", () => {
  it("tells that the hits answer an earlier query until the new answer comes", async () => {
    api.results = { old: [hit("a", 1)], new: [hit("b", 1)] };
    box.input("old");
    await vi.advanceTimersByTimeAsync(150);
    expect(box.stale).toBe(false);

    box.input("new");
    expect(box.stale).toBe(true);
    expect(box.hits.map((h) => h.noteId)).toEqual(["a"]);

    await vi.advanceTimersByTimeAsync(150);
    expect(box.stale).toBe(false);
  });

  it("fresh searches the query at once and gives the chosen hit of its answer", async () => {
    api.results = { old: [hit("a", 1)], new: [hit("b", 1), hit("c", 2)] };
    box.input("old");
    await vi.advanceTimersByTimeAsync(150);
    box.input("new");

    const got = await box.fresh();

    expect(api.calls).toEqual(["old", "new"]);
    expect(got?.noteId).toBe("b");
    await vi.advanceTimersByTimeAsync(150);
    expect(api.calls).toEqual(["old", "new"]);
  });

  it("fresh gives the chosen hit at once when the hits answer the query", async () => {
    api.results = { word: [hit("a", 1), hit("b", 2)] };
    box.input("word");
    await vi.advanceTimersByTimeAsync(150);
    box.move(1);

    expect((await box.fresh())?.noteId).toBe("b");
    expect(api.calls).toEqual(["word"]);
  });

  it("fresh gives nothing when the query changes before the answer", async () => {
    api.hold = true;
    box.input("new");
    const got = box.fresh();
    box.input("newer");
    api.pending[0].resolve([hit("b", 1)]);

    expect(await got).toBeUndefined();
  });

  it("debounces input", async () => {
    box.input("n");
    await vi.advanceTimersByTimeAsync(50);
    box.input("ne");
    await vi.advanceTimersByTimeAsync(50);
    box.input("nee");
    await vi.advanceTimersByTimeAsync(149);
    expect(api.calls).toEqual([]);

    await vi.advanceTimersByTimeAsync(1);
    expect(api.calls).toEqual(["nee"]);
  });

  it("drops stale responses", async () => {
    api.hold = true;
    box.input("first");
    await vi.advanceTimersByTimeAsync(150);
    box.input("second");
    await vi.advanceTimersByTimeAsync(150);
    expect(api.pending.map((p) => p.query)).toEqual(["first", "second"]);

    api.pending[1].resolve([hit("b", 2)]);
    await vi.advanceTimersByTimeAsync(0);
    api.pending[0].resolve([hit("a", 1)]);
    await vi.advanceTimersByTimeAsync(0);

    expect(box.hits.map((h) => h.noteId)).toEqual(["b"]);
  });

  it("does not search for a blank query and clears the results", async () => {
    api.results.word = [hit("a", 1)];
    box.input("word");
    await vi.advanceTimersByTimeAsync(150);
    expect(box.hits).toHaveLength(1);

    box.input("  ");
    await vi.advanceTimersByTimeAsync(150);
    expect(api.calls).toEqual(["word"]);
    expect(box.hits).toEqual([]);
    expect(box.current()).toBeUndefined();
  });

  it("treats null results from Go as none", async () => {
    api.results.word = null;
    box.input("word");
    await vi.advanceTimersByTimeAsync(150);
    expect(box.hits).toEqual([]);
  });

  it("keeps at most 10 hits", async () => {
    api.results.word = Array.from({ length: 12 }, (_, i) => hit(`n${i}`, 1));
    box.input("word");
    await vi.advanceTimersByTimeAsync(150);
    expect(box.hits).toHaveLength(10);
  });

  it("moves selection within bounds", async () => {
    api.results.word = [hit("a", 1), hit("b", 2), hit("c", 3)];
    box.input("word");
    await vi.advanceTimersByTimeAsync(150);
    expect(box.current()?.noteId).toBe("a");

    box.move(-1);
    expect(box.selected).toBe(0);
    box.move(1);
    box.move(1);
    box.move(1);
    expect(box.selected).toBe(2);
    expect(box.current()?.noteId).toBe("c");
    box.move(-1);
    expect(box.current()?.noteId).toBe("b");
  });

  it("move does nothing without hits", () => {
    box.move(1);
    expect(box.selected).toBe(-1);
    expect(box.current()).toBeUndefined();
  });

  it("again searches the current query at once and returns its hits", async () => {
    api.results.word = [hit("a", 1)];
    box.input("word");
    const hits = await box.again();

    expect(api.calls).toEqual(["word"]);
    expect(hits.map((h) => h.noteId)).toEqual(["a"]);
    expect(box.hits.map((h) => h.noteId)).toEqual(["a"]);
    await vi.advanceTimersByTimeAsync(150);
    expect(api.calls).toEqual(["word"]);
  });

  it("a failed search shows no hits and again returns none", async () => {
    api.fail = true;
    box.input("word");
    await expect(box.again()).resolves.toEqual([]);
    expect(box.hits).toEqual([]);
    expect(box.statusLine()).toBe("Search unavailable");
  });

  it("a search the worker cannot run says Search unavailable, not No matches", async () => {
    box.follow(ready);
    api.fail = true;
    api.code = "search_unavailable";
    box.input("word");
    await vi.advanceTimersByTimeAsync(150);
    expect(box.hits).toEqual([]);
    expect(box.statusLine()).toBe("Search unavailable");
  });

  describe("search again when the word search is back", () => {
    const down: SearchStatusDTO = { ...ready, lexical: "unavailable", semantic: "unavailable" };

    it("after a restart", async () => {
      box.follow(ready);
      box.follow(down);
      api.fail = true;
      api.code = "search_unavailable";
      box.input("word");
      await vi.advanceTimersByTimeAsync(150);
      expect(box.statusLine()).toBe("Search unavailable");

      api.fail = false;
      api.results.word = [hit("a", 1)];
      box.follow(ready);
      await vi.advanceTimersByTimeAsync(0);
      expect(api.calls).toEqual(["word", "word"]);
      expect(box.hits.map((h) => h.noteId)).toEqual(["a"]);
      expect(box.statusLine()).toBe("");
    });

    it("after the first start", async () => {
      box.seed({ notes: 0, queued: 0, lexical: "loading", semantic: "loading" });
      api.fail = true;
      box.input("word");
      await vi.advanceTimersByTimeAsync(150);

      api.fail = false;
      api.results.word = [hit("a", 1)];
      box.follow({ ...ready, semantic: "loading" });
      await vi.advanceTimersByTimeAsync(0);
      expect(api.calls).toEqual(["word", "word"]);
      expect(box.hits.map((h) => h.noteId)).toEqual(["a"]);
    });

    it("after a search the worker could not run, even when the unavailable status was not seen", async () => {
      box.follow(ready);
      api.fail = true;
      api.code = "search_unavailable";
      box.input("word");
      await vi.advanceTimersByTimeAsync(150);

      api.fail = false;
      box.follow({ ...ready, notes: 501 });
      await vi.advanceTimersByTimeAsync(0);
      expect(api.calls).toEqual(["word", "word"]);
      expect(box.statusLine()).toBe("No matches");
    });

    it("not after a search that failed for another reason", async () => {
      box.follow(ready);
      api.fail = true;
      api.code = "internal";
      box.input("word");
      await vi.advanceTimersByTimeAsync(150);

      box.follow({ ...ready, queued: 3 });
      await vi.advanceTimersByTimeAsync(0);
      expect(api.calls).toEqual(["word"]);
      expect(box.statusLine()).toBe("Search unavailable");
    });

    it("not while it stays ready, nor without a query", async () => {
      box.follow(ready);
      box.input("word");
      await vi.advanceTimersByTimeAsync(150);
      box.follow({ ...ready, queued: 3 });
      await vi.advanceTimersByTimeAsync(0);
      expect(api.calls).toEqual(["word"]);

      box.input("");
      box.follow(down);
      box.follow(ready);
      await vi.advanceTimersByTimeAsync(0);
      expect(api.calls).toEqual(["word"]);
    });
  });

  // A query typed while the model loads has no semantic hits; once the model
  // is ready it is searched once more, not on every later status.
  it("searches again once when the semantic search turns ready", async () => {
    const loading: SearchStatusDTO = { ...ready, semantic: "loading" };
    box.follow(loading);
    box.input("word");
    await vi.advanceTimersByTimeAsync(150);
    expect(api.calls).toEqual(["word"]);

    api.results.word = [hit("a", 1)];
    box.follow(ready);
    await vi.advanceTimersByTimeAsync(0);
    expect(api.calls).toEqual(["word", "word"]);
    expect(box.hits.map((h) => h.noteId)).toEqual(["a"]);

    box.follow({ ...ready, queued: 3 });
    box.follow(ready);
    box.follow({ ...ready, semantic: "unavailable" });
    await vi.advanceTimersByTimeAsync(0);
    expect(api.calls).toEqual(["word", "word"]);
  });

  it("status from Status() does not replace a newer event", () => {
    const event = { ...ready, queued: 10 };
    box.follow(event);
    box.seed(ready);
    expect(box.status).toEqual(event);

    const fresh = new SearchBox(api);
    fresh.seed(ready);
    expect(fresh.status).toEqual(ready);
  });

  describe("status line texts", () => {
    it.each([
      { name: "nothing known", status: undefined, line: "" },
      { name: "ready", status: ready, line: "" },
      { name: "indexing", status: { ...ready, queued: 380 }, line: "Indexing 120 of 500" },
      { name: "semantic loading", status: { ...ready, queued: 500, semantic: "loading" }, line: "Semantic search is loading" },
      { name: "semantic unavailable", status: { ...ready, semantic: "unavailable" }, line: "Semantic search unavailable" },
      { name: "lexical unavailable", status: { ...ready, queued: 3, lexical: "unavailable", semantic: "unavailable" }, line: "Search unavailable" },
    ])("$name → '$line'", ({ status, line }) => {
      if (status) box.follow(status);
      expect(box.statusLine()).toBe(line);
    });

    it("No matches for a query without hits, before the indexing status", async () => {
      box.follow({ ...ready, queued: 380 });
      box.input("nothing");
      expect(box.statusLine()).toBe("Indexing 120 of 500");

      await vi.advanceTimersByTimeAsync(150);
      expect(box.statusLine()).toBe("No matches");
    });

    it("no No matches while the answer to the current query is pending", async () => {
      box.input("nothing");
      await vi.advanceTimersByTimeAsync(150);
      box.input("nothing more");
      expect(box.statusLine()).toBe("");
    });

    it("Search unavailable before No matches", async () => {
      box.follow({ ...ready, lexical: "unavailable" });
      box.input("nothing");
      await vi.advanceTimersByTimeAsync(150);
      expect(box.statusLine()).toBe("Search unavailable");
    });
  });
});

describe("highlight", () => {
  it("splits the snippet into marked and plain pieces", () => {
    expect(highlight("one two three", [[4, 7]])).toEqual([
      { text: "one ", mark: false },
      { text: "two", mark: true },
      { text: " three", mark: false },
    ]);
  });

  it("sorts, merges overlaps and clamps marks to the snippet", () => {
    expect(highlight("abcdef", [[4, 9], [0, 2], [1, 3]])).toEqual([
      { text: "abc", mark: true },
      { text: "d", mark: false },
      { text: "ef", mark: true },
    ]);
  });

  it("ignores empty or reversed marks and null", () => {
    expect(highlight("abc", [[2, 2], [2, 1]])).toEqual([{ text: "abc", mark: false }]);
    expect(highlight("abc", null)).toEqual([{ text: "abc", mark: false }]);
  });
});

describe("isFindShortcut", () => {
  const key = (init: KeyboardEventInit) => ({ key: "f", code: "KeyF", ctrlKey: false, metaKey: false, altKey: false, shiftKey: false, ...init });

  it("is Ctrl+F off macOS and Cmd+F on macOS", () => {
    expect(isFindShortcut(key({ ctrlKey: true }), false)).toBe(true);
    expect(isFindShortcut(key({ key: "F", ctrlKey: true }), false)).toBe(true);
    expect(isFindShortcut(key({ metaKey: true }), false)).toBe(false);
    expect(isFindShortcut(key({ metaKey: true }), true)).toBe(true);
    expect(isFindShortcut(key({ ctrlKey: true }), true)).toBe(false);
  });

  it("is the F key in a Cyrillic layout, where the key is а", () => {
    expect(isFindShortcut(key({ key: "а", ctrlKey: true }), false)).toBe(true);
    expect(isFindShortcut(key({ key: "а", metaKey: true }), true)).toBe(true);
    expect(isFindShortcut(key({ key: "а", metaKey: true }), false)).toBe(false);
    expect(isFindShortcut(key({ key: "а", ctrlKey: true, altKey: true }), false)).toBe(false);
    expect(isFindShortcut(key({ key: "п", code: "KeyG", ctrlKey: true }), false)).toBe(false);
  });

  it("is not F with Alt or Shift, nor another key", () => {
    expect(isFindShortcut(key({ ctrlKey: true, altKey: true }), false)).toBe(false);
    expect(isFindShortcut(key({ ctrlKey: true, shiftKey: true }), false)).toBe(false);
    expect(isFindShortcut(key({ key: "g", code: "KeyG", ctrlKey: true }), false)).toBe(false);
  });
});
