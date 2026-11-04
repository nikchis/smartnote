// @vitest-environment happy-dom
import { flushSync, mount, unmount } from "svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SearchHitDTO } from "../lib/api";
import { SearchBox, type SearchApi } from "../lib/search.svelte";
import SearchBar from "./SearchBar.svelte";

class FakeSearch implements SearchApi {
  results: SearchHitDTO[] = [];
  async Search() {
    return this.results;
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

describe("SearchBar", () => {
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

  it("takes at most the 256 UTF-16 units of a query that the worker reads", () => {
    // maxlength counts UTF-16 units, as the worker and SearchService.Search do.
    expect(field().maxLength).toBe(256);
  });
});
