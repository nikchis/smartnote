// @vitest-environment happy-dom
import { flushSync, mount, tick, unmount } from "svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { NoteDTO, SearchHitDTO } from "../lib/api";
import { answer as answerDialog, currentDialog } from "../lib/dialogs.svelte";
import { i18n, msg } from "../lib/i18n.svelte";

// Main talks to Go through the generated bindings and the Wails events: both
// are replaced; the notes and the answers of the search come from state.
const state = vi.hoisted(() => ({
  notes: [] as NoteDTO[],
  search: (async () => []) as (q: string) => Promise<SearchHitDTO[]>,
  setLanguage: (async () => {}) as (lang: string) => Promise<void>,
  languages: [] as string[],
}));

vi.mock("../lib/api", () => ({
  NotesService: { List: async () => state.notes, Versions: async () => [] },
  SearchService: {
    Search: (q: string) => state.search(q),
    Status: async () => ({ notes: 2, queued: 0, lexical: "ready", semantic: "ready" }),
  },
}));
vi.mock("@wailsio/runtime", () => ({ Events: { On: () => () => {} } }));

const { default: Main } = await import("./Main.svelte");

// Go saves the language; state answers for it.
i18n.use({
  Language: async () => "en",
  SetLanguage: (lang: string) => {
    state.languages.push(lang);
    return state.setLanguage(lang);
  },
});

function hit(noteId: string, start: number, text: string): SearchHitDTO {
  return { noteId, line: 1, start, end: start + text.length, text, snippet: text, marks: [], sources: ["words"] };
}

const settle = async () => {
  for (let i = 0; i < 3; i++) {
    await new Promise((r) => setTimeout(r, 0));
    await tick();
    flushSync();
  }
};

let target: HTMLElement;
let screen: ReturnType<typeof mount>;

beforeEach(async () => {
  state.setLanguage = async () => {};
  state.languages = [];
  state.notes = [
    { id: "a", name: "alpha", info: "first", updated: 1 },
    { id: "b", name: "beta", info: "prefix needle", updated: 2 },
  ];
  target = document.createElement("div");
  document.body.append(target);
  screen = mount(Main, {
    target,
    props: {
      sync: { connected: false, dirty: false, restoreAvailable: false, busy: false } as never,
      onSettings: () => {},
      onRestore: () => {},
      onBackup: () => {},
    },
  });
  await settle();
});

afterEach(async () => {
  while (currentDialog()) answerDialog("yes");
  await unmount(screen);
  target.remove();
  state.setLanguage = async () => {};
  await i18n.set("en");
});

const field = () => target.querySelector<HTMLInputElement>('[role="search"] input')!;
const text = () => target.querySelector("textarea")!;
const toggle = () => target.querySelector<HTMLButtonElement>('button[aria-label^="Language"], button[aria-label^="Язык"]')!;

const privacy = () => target.querySelector<HTMLButtonElement>('.tools button[aria-label="Privacy Policy"], .tools button[aria-label="Политика конфиденциальности"]')!;

async function search(q: string): Promise<void> {
  field().focus();
  field().value = q;
  field().dispatchEvent(new Event("input", { bubbles: true }));
  await new Promise((r) => setTimeout(r, 170)); // past the debounce
  await settle();
}

function enter(): void {
  field().dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
}

describe("Main", () => {
  // New and Remove act on the list of notes, so they sit above it, left of
  // the search, in a column as wide as the list; the editor keeps Apply and
  // Cancel (decision of 2026-11-07).
  it("New and Remove sit above the list, left of the search", () => {
    const top = target.querySelector(".find")!;
    const notes = top.querySelector(".notes")!;
    expect([...notes.querySelectorAll("button")].map((b) => b.getAttribute("title"))).toEqual(["Add a new note", "Remove current note"]);
    expect(notes.compareDocumentPosition(top.querySelector('[role="search"]')!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(field().placeholder).toBe("Search records");
    expect(field().getAttribute("aria-label")).toBe("Search records");
    expect([...target.querySelectorAll(".editor .actions button")].map((b) => b.textContent?.trim())).toEqual(["Apply changes", "Cancel changes"]);
  });

  // The footer button sends the data to the remote storage: it is named
  // after the Backup it runs, not Save, which the editor's Apply does.
  it("the footer names the remote copy Backup", () => {
    const labels = [...target.querySelectorAll("footer button")].map((b) => [b.textContent?.trim(), b.getAttribute("title")]);
    expect(labels).toContainEqual(["Backup", "Back up data to remote storage"]);
    expect(labels.map(([text]) => text)).not.toContain("Save");
  });

  // Master audit 2026-11-07, item 1: App asks through discardDraft before a
  // Restore or closing replaces the editor's fields.
  it("discardDraft asks about an edited note; Cancel keeps the draft", async () => {
    await expect((screen as { discardDraft(): Promise<boolean> }).discardDraft()).resolves.toBe(true);
    expect(currentDialog()).toBeUndefined();

    text().value = "first, edited";
    text().dispatchEvent(new Event("input", { bubbles: true }));
    await settle();

    const kept = (screen as { discardDraft(): Promise<boolean> }).discardDraft();
    await settle();
    expect(currentDialog()?.message).toEqual(msg("editor.discard"));
    answerDialog("no");
    await expect(kept).resolves.toBe(false);
    expect(text().value).toBe("first, edited");
  });

  // From the review of PR 6: the jump to an outdated hit of note a searches
  // again; meanwhile the user opens a hit of note b. The late answer for a
  // must not take the selection, the focus or the scroll of b.
  it("a late retry of an old jump leaves the place of a newer jump", async () => {
    let answer!: (hits: SearchHitDTO[]) => void;
    let retried!: () => void;
    const retrying = new Promise<void>((r) => (retried = r));
    let calls = 0;
    state.search = async (q) => {
      if (q === "needle") return [hit("b", 7, "needle")];
      if (++calls === 1) return [hit("a", 1, "first")]; // outdated: "first" is at 0
      retried();
      return new Promise((r) => (answer = r));
    };

    await search("first");
    enter();
    await retrying;

    await search("needle");
    enter();
    await settle();
    expect([text().value, text().selectionStart, text().selectionEnd]).toEqual(["prefix needle", 7, 13]);

    answer([hit("a", 0, "first")]);
    await settle();

    expect([text().value, text().selectionStart, text().selectionEnd]).toEqual(["prefix needle", 7, 13]);
  });

  // Spec §16.4: the language button is right of the search, before the
  // buttons that come later at the right end of the row.
  it("language toggle right after search", () => {
    const top = target.querySelector(".find")!;
    const search = top.querySelector('[role="search"]')!;
    expect(search.nextElementSibling?.firstElementChild).toBe(toggle());
  });

  it("top bar order", () => {
    const tools = target.querySelector(".find")!.querySelector('[role="search"]')!.nextElementSibling!;
    expect([...tools.children]).toEqual([toggle(), privacy()]);
    expect(target.querySelector(".find")!.lastElementChild).toBe(tools);
  });

  it("privacy button is icon-only and last", async () => {
    expect(privacy().getAttribute("aria-label")).toBe("Privacy Policy");
    expect(privacy().textContent?.trim()).toBe("");
    expect(privacy().querySelector("img")).not.toBeNull();
    privacy().click();
    await settle();
    expect(target.querySelector('[role="dialog"]')).not.toBeNull();
    toggle().click();
    await settle();
    expect(privacy().getAttribute("aria-label")).toBe("Политика конфиденциальности");
  });

  it("language toggle shows current and switches", async () => {
    expect(toggle().textContent?.trim()).toBe("EN");
    expect(toggle().getAttribute("title")).toBe("Language: English");

    toggle().click();
    await settle();

    expect(state.languages).toEqual(["ru"]);
    expect(toggle().textContent?.trim()).toBe("RU");
    expect(toggle().getAttribute("aria-label")).toBe("Язык: русский");
    expect([...target.querySelectorAll(".notes button")].map((b) => b.textContent?.trim())).toEqual(["Новая", "Удалить"]);
    expect(field().placeholder).toBe("Поиск по заметкам");
    expect([...target.querySelectorAll("footer button")].map((b) => b.textContent?.trim())).toContain("Резервная копия");
  });

  // Task 13: when Go cannot save the language it keeps the old one, so must the interface.
  it("language toggle keeps the language and shows the error when Go cannot save it", async () => {
    const messages = { en: "Cannot save the settings.", ru: "Не удалось сохранить настройки." };
    state.setLanguage = async () => {
      throw new Error(JSON.stringify({ code: "internal", message: messages.en, messages }));
    };

    toggle().click();
    await settle();

    expect(toggle().textContent?.trim()).toBe("EN");
    expect(currentDialog()).toEqual({ kind: "error", message: { messages } });
  });

  it("shows the date of the note in the language of the interface", async () => {
    const updated = () => target.querySelector<HTMLInputElement>("input.updated")!.value;
    expect(updated()).toBe(new Date(1).toLocaleString("en"));

    toggle().click();
    await settle();

    expect(updated()).toBe(new Date(1).toLocaleString("ru"));
  });
});
