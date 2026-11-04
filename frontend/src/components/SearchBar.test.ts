// @vitest-environment happy-dom
import { flushSync, mount, unmount } from "svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SearchHitDTO } from "../lib/api";
import { SearchBox, type SearchApi } from "../lib/search.svelte";
import SearchBar from "./SearchBar.svelte";

class FakeSearch implements SearchApi {
  results: SearchHitDTO[] = [];
  /** byQuery answers a query with its own hits; other queries get results. */
  byQuery: Record<string, SearchHitDTO[]> = {};
  calls: string[] = [];
  async Search(q: string) {
    this.calls.push(q);
    return this.byQuery[q] ?? this.results;
  }
}

function hit(noteId: string): SearchHitDTO {
  return { noteId, line: 1, start: 0, end: 4, text: "word", snippet: "word", marks: [[0, 4]], sources: ["words"] };
}

let target: HTMLElement;
let bar: ReturnType<typeof SearchBar>;
let api: FakeSearch;
let box: SearchBox;
let opened: SearchHitDTO[];

beforeEach(() => {
  vi.useFakeTimers();
  api = new FakeSearch();
  box = new SearchBox(api);
  opened = [];
  target = document.createElement("div");
  document.body.append(target);
  bar = mount(SearchBar, { target, props: { box, nameOf: (id: string) => `note ${id}`, onOpen: (h: SearchHitDTO) => opened.push(h) } });
});

afterEach(() => {
  unmount(bar);
  target.remove();
  vi.useRealTimers();
});

const field = () => target.querySelector("input")!;
const options = () => [...target.querySelectorAll('[role="option"]')].map((o) => o.querySelector(".note")?.textContent);

async function type(q: string): Promise<void> {
  field().value = q;
  field().dispatchEvent(new Event("input", { bubbles: true }));
  await vi.advanceTimersByTimeAsync(150);
  flushSync();
}

function press(key: string): void {
  field().dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true }));
  flushSync();
}

/** typeAhead puts q in the field without waiting for the search delay. */
function typeAhead(q: string): void {
  field().value = q;
  field().dispatchEvent(new Event("input", { bubbles: true }));
  flushSync();
}

describe("SearchBar", () => {
  // The hits on screen answer the previous query until the new answer comes:
  // Enter must not open one of them for the new query.
  it("opens with Enter a hit of the query in the field, not of the previous one", async () => {
    api.byQuery = { old: [hit("a")], new: [hit("b")] };
    field().focus();
    await type("old");
    expect(options()).toEqual(["note a"]);

    typeAhead("new");
    press("Enter");
    await vi.advanceTimersByTimeAsync(0);
    flushSync();

    expect(api.calls).toEqual(["old", "new"]);
    expect(opened.map((h) => h.noteId)).toEqual(["b"]);
  });

  it("opens nothing with Enter when the field changed again before the answer", async () => {
    api.byQuery = { old: [hit("a")], new: [hit("b")], newer: [hit("c")] };
    field().focus();
    await type("old");

    typeAhead("new");
    press("Enter");
    typeAhead("newer");
    await vi.advanceTimersByTimeAsync(0);
    flushSync();

    expect(opened).toEqual([]);
  });

  it("shows the hits of the previous query as stale and does not open them on a click", async () => {
    api.byQuery = { old: [hit("a")], new: [hit("b")] };
    field().focus();
    await type("old");

    typeAhead("new");
    const row = target.querySelector<HTMLElement>('[role="option"]')!;
    expect(row.closest('[role="listbox"]')?.getAttribute("aria-busy")).toBe("true");
    row.click();
    flushSync();

    expect(opened).toEqual([]);
  });

  it("opens the chosen hit with Enter and closes the list", async () => {
    api.results = [hit("a"), hit("b")];
    field().focus();
    await type("word");
    expect(options()).toEqual(["note a", "note b"]);

    press("ArrowDown");
    press("Enter");

    expect(opened.map((h) => h.noteId)).toEqual(["b"]);
    expect(options()).toEqual([]);
  });

  it("shows the renewed hits again after a gone note", async () => {
    api.results = [hit("gone"), hit("b")];
    field().focus();
    await type("word");
    press("Enter");
    expect(options()).toEqual([]);

    // Main: the note is gone, the editor searched again, the field still has the focus.
    api.results = [hit("b")];
    await box.again();
    bar.focus();
    flushSync();

    expect(document.activeElement).toBe(field());
    expect(options()).toEqual(["note b"]);
  });

  // From the review of PR 6: the list is at most 70vh high, so the row the
  // arrow keys choose must be scrolled into view.
  it("scrolls the row chosen with the arrow keys into view", async () => {
    const scrolled: Element[] = [];
    const spy = vi.spyOn(HTMLElement.prototype, "scrollIntoView").mockImplementation(function (this: HTMLElement) {
      scrolled.push(this);
    });
    try {
      api.results = Array.from({ length: 10 }, (_, i) => hit(String(i)));
      field().focus();
      await type("word");

      press("ArrowDown");
      press("ArrowDown");
      await vi.advanceTimersByTimeAsync(0);
      flushSync();

      expect(scrolled.at(-1)?.id).toBe("search-hit-2");
      press("ArrowUp");
      await vi.advanceTimersByTimeAsync(0);
      flushSync();
      expect(scrolled.at(-1)?.id).toBe("search-hit-1");
    } finally {
      spy.mockRestore();
    }
  });

  it("takes at most the 256 UTF-16 units of a query that the worker reads", () => {
    // maxlength counts UTF-16 units, as the worker and SearchService.Search do.
    expect(field().maxLength).toBe(256);
  });
});
