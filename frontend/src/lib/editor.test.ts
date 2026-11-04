import { beforeEach, describe, expect, it } from "vitest";
import type { NoteDTO, SearchHitDTO, VersionDTO } from "./api";
import type { Dialogs, Labels } from "./dialogs.svelte";
import { Editor, type NotesApi } from "./editor.svelte";
import { SearchBox, type SearchApi } from "./search.svelte";

class FakeNotes implements NotesApi {
  notes: NoteDTO[] = [];
  versions: Record<string, VersionDTO[]> = {};
  calls: string[] = [];
  next = 1;

  async List(): Promise<NoteDTO[] | null> {
    this.calls.push("List");
    return [...this.notes].sort((a, b) => a.name.localeCompare(b.name));
  }
  async Create(name: string, info: string) {
    this.calls.push(`Create ${name}`);
    const n = { id: `id${this.next++}`, name, info, updated: 1000 };
    this.notes.push(n);
    return n;
  }
  async Save(id: string, name: string, info: string) {
    this.calls.push(`Save ${id}`);
    const i = this.notes.findIndex((n) => n.id === id);
    const prev = this.notes[i];
    if (prev.info !== info) {
      (this.versions[id] ??= []).unshift({ id: `v${this.next++}`, created: prev.updated, info: prev.info });
    }
    this.notes[i] = { id, name, info, updated: 2000 };
    return this.notes[i];
  }
  async Delete(id: string) {
    this.calls.push(`Delete ${id}`);
    this.notes = this.notes.filter((n) => n.id !== id);
  }
  async Versions(id: string): Promise<VersionDTO[] | null> {
    return this.versions[id] ?? [];
  }
}

class FakeDialogs implements Dialogs {
  shown: string[] = [];
  labels: (Labels | undefined)[] = [];
  answer = true;
  async info(msg: string) {
    this.shown.push(`info: ${msg}`);
  }
  async error(msg: string) {
    this.shown.push(`error: ${msg}`);
  }
  async ask(msg: string, labels?: Labels) {
    this.labels.push(labels);
    this.shown.push(labels ? `ask: ${msg} [${labels.yes}/${labels.no}]` : `ask: ${msg}`);
    return this.answer;
  }
  async choose(msg: string) {
    this.shown.push(`choose: ${msg}`);
    return this.answer ? ("yes" as const) : ("no" as const);
  }
}

class FakeSearch implements SearchApi {
  calls: string[] = [];
  results: SearchHitDTO[] = [];
  async Search(q: string) {
    this.calls.push(q);
    return this.results;
  }
}

function hit(noteId: string, line: number, start: number, text: string): SearchHitDTO {
  return { noteId, line, start, end: start + text.length, text, snippet: text, marks: [], sources: ["words"] };
}

const DISCARD = "ask: Discard unsaved changes? [Discard/Cancel]";

let api: FakeNotes;
let d: FakeDialogs;
let search: FakeSearch;
let box: SearchBox;
let ed: Editor;

beforeEach(async () => {
  api = new FakeNotes();
  api.notes = [
    { id: "a", name: "alpha", info: "first", updated: 1 },
    { id: "b", name: "beta", info: "second", updated: 2 },
  ];
  d = new FakeDialogs();
  search = new FakeSearch();
  box = new SearchBox(search);
  ed = new Editor(api, d, box);
  await ed.load();
});

describe("Editor", () => {
  it("selects the first note after loading", () => {
    expect(ed.notes.map((n) => n.name)).toEqual(["alpha", "beta"]);
    expect(ed.selected?.id).toBe("a");
    expect(ed.state).toMatchObject({ mode: "edit", name: "alpha", info: "first" });
  });

  it("apply in new mode creates the note and selects it", async () => {
    ed.startNew();
    ed.name = "gamma";
    ed.info = "third";
    await ed.apply();

    expect(api.calls).toContain("Create gamma");
    expect(d.shown).toEqual(["info: Note has been successfully added."]);
    expect(ed.state).toMatchObject({ mode: "edit", name: "gamma", savedName: "gamma", savedInfo: "third" });
    expect(ed.notes.map((n) => n.name)).toEqual(["alpha", "beta", "gamma"]);
  });

  it("apply in edit mode saves and keeps a version of the old text", async () => {
    await ed.select("a");
    ed.info = "changed";
    await ed.apply();

    expect(api.calls).toContain("Save a");
    expect(d.shown).toEqual(["info: Note has been successfully updated."]);
    expect(ed.state).toMatchObject({ mode: "edit", savedInfo: "changed", hasVersions: true });
  });

  it("remove asks and does nothing on No", async () => {
    await ed.select("a");
    d.answer = false;
    await ed.remove();

    expect(d.shown).toEqual(["ask: Are you sure you want to remove this note?"]);
    expect(api.calls).not.toContain("Delete a");
    expect(ed.state.mode).toBe("edit");
  });

  it("remove on Yes deletes and selects the first remaining note", async () => {
    await ed.select("a");
    await ed.remove();

    expect(api.calls).toContain("Delete a");
    expect(d.shown).toEqual(["ask: Are you sure you want to remove this note?", "info: Note has been successfully removed."]);
    expect(ed.state).toMatchObject({ mode: "edit", name: "beta" });
    expect(ed.notes.map((n) => n.id)).toEqual(["b"]);
  });

  it("select from list asks with draft", async () => {
    await ed.select("a");
    ed.info = "unsaved";
    d.answer = false;

    await expect(ed.select("b")).resolves.toBe(false);
    expect(d.shown).toEqual([DISCARD]);
    expect(ed.selected?.id).toBe("a");
    expect(ed.state).toMatchObject({ mode: "edit", name: "alpha", info: "unsaved" });

    d.answer = true;
    await expect(ed.select("b")).resolves.toBe(true);
    expect(ed.state).toMatchObject({ mode: "edit", name: "beta", info: "second" });
    expect(api.notes.find((n) => n.id === "a")?.info).toBe("first");
  });

  it("select without a draft does not ask", async () => {
    await expect(ed.select("b")).resolves.toBe(true);
    expect(d.shown).toEqual([]);
    expect(ed.selected?.id).toBe("b");
  });

  it("select of the selected note keeps its draft without asking", async () => {
    ed.info = "unsaved";
    await expect(ed.select("a")).resolves.toBe(true);
    expect(d.shown).toEqual([]);
    expect(ed.info).toBe("unsaved");
  });

  it("step keeps the selection when the draft is kept", async () => {
    ed.name = "renamed";
    d.answer = false;
    await ed.step(1);

    expect(ed.selected?.id).toBe("a");
    expect(ed.name).toBe("renamed");
  });

  it("openHit asks before replacing draft of another note", async () => {
    ed.info = "unsaved";
    d.answer = false;
    await expect(ed.openHit(hit("b", 1, 0, "second"))).resolves.toBe("cancelled");
    expect(d.shown).toEqual([DISCARD]);
    expect(d.labels).toEqual([{ yes: "Discard", no: "Cancel", primary: "no" }]);
    expect(ed.state).toMatchObject({ mode: "edit", name: "alpha", info: "unsaved" });

    d.answer = true;
    await expect(ed.openHit(hit("b", 1, 0, "second"))).resolves.toBe("selected");
    expect(ed.state).toMatchObject({ mode: "edit", name: "beta", info: "second" });
    expect(ed.place).toEqual({ field: "info", start: 0, end: 6 });
  });

  it("openHit asks before replacing the draft of a new note", async () => {
    ed.startNew();
    ed.info = "draft";
    d.answer = false;
    await expect(ed.openHit(hit("b", 1, 0, "second"))).resolves.toBe("cancelled");
    expect(ed.state).toMatchObject({ mode: "new", info: "draft" });

    d.answer = true;
    await expect(ed.openHit(hit("b", 1, 0, "second"))).resolves.toBe("selected");
    expect(ed.state).toMatchObject({ mode: "edit", name: "beta" });
  });

  it("openHit from an empty new note does not ask", async () => {
    ed.startNew();
    await expect(ed.openHit(hit("b", 0, 0, "beta"))).resolves.toBe("selected");
    expect(d.shown).toEqual([]);
    expect(ed.place).toEqual({ field: "name", start: 0, end: 4 });
  });

  it("openHit asks for the same note with draft", async () => {
    ed.info = "first, edited";
    d.answer = false;
    await expect(ed.openHit(hit("a", 1, 0, "first"))).resolves.toBe("cancelled");
    expect(d.shown).toEqual([DISCARD]);
    expect(ed.state).toMatchObject({ mode: "edit", name: "alpha", info: "first, edited" });

    d.answer = true;
    await expect(ed.openHit(hit("a", 1, 0, "first"))).resolves.toBe("selected");
    expect(ed.state).toMatchObject({ mode: "edit", info: "first", savedInfo: "first" });
  });

  it("openHit in the open note without a draft selects without asking", async () => {
    await expect(ed.openHit(hit("a", 0, 1, "lph"))).resolves.toBe("selected");
    expect(d.shown).toEqual([]);
    expect(ed.place).toEqual({ field: "name", start: 1, end: 4 });
  });

  it("openHit retries when text changed", async () => {
    api.notes[1] = { ...api.notes[1], info: "zero\nsecond line\nthird" };
    await ed.load();
    box.input("second");
    search.results = [hit("a", 1, 0, "first"), hit("b", 3, 12, "second"), hit("b", 2, 5, "second")];

    await expect(ed.openHit(hit("b", 1, 0, "second"))).resolves.toBe("selected");
    expect(search.calls).toEqual(["second"]);
    expect(ed.state).toMatchObject({ mode: "edit", name: "beta" });
    expect(ed.place).toEqual({ field: "info", start: 5, end: 11 });
  });

  it("openHit leaves the note unselected when the retry finds no match", async () => {
    box.input("second");
    search.results = [hit("b", 1, 2, "second")];

    await expect(ed.openHit(hit("b", 1, 1, "second"))).resolves.toBe("unselected");
    expect(search.calls).toEqual(["second"]);
    expect(ed.state).toMatchObject({ mode: "edit", name: "beta" });
    expect(ed.place).toBeUndefined();
  });

  it("openHit gone", async () => {
    ed.info = "unsaved";
    box.input("word");
    await expect(ed.openHit(hit("x", 1, 0, "word"))).resolves.toBe("gone");
    expect(d.shown).toEqual([]);
    expect(search.calls).toEqual(["word"]);
    expect(ed.state).toMatchObject({ mode: "edit", name: "alpha", info: "unsaved" });
  });

  it("openHit replaced by a newer jump while it searches again changes nothing", async () => {
    let answer!: (hits: SearchHitDTO[]) => void;
    search.Search = (q: string) => {
      search.calls.push(q);
      return new Promise((r) => (answer = r));
    };
    box.input("first");
    const old = ed.openHit(hit("a", 1, 1, "first")); // outdated: "first" is at 0
    await Promise.resolve();
    search.Search = async () => [];

    await expect(ed.openHit(hit("b", 1, 0, "second"))).resolves.toBe("selected");
    answer([hit("a", 1, 0, "first")]);

    await expect(old).resolves.toBe("cancelled");
    expect(ed.state).toMatchObject({ mode: "edit", name: "beta" });
    expect(ed.place).toEqual({ field: "info", start: 0, end: 6 });
  });

  it("useVersion puts the text into the editor without saving", async () => {
    await ed.select("a");
    ed.useVersion("old text");

    expect(ed.info).toBe("old text");
    expect(api.calls.filter((c) => c.startsWith("Save"))).toEqual([]);
    expect(ed.buttons.apply).toBe(true);
  });

  it("select is ignored while a new note is being created", async () => {
    ed.startNew();
    ed.name = "draft";
    await ed.select("a");

    expect(ed.state).toMatchObject({ mode: "new", name: "draft" });
  });

  it("step moves the selection to the next or previous note and stops at the ends", async () => {
    await ed.step(1);
    expect(ed.selected?.id).toBe("b");

    await ed.step(1);
    expect(ed.selected?.id).toBe("b");

    await ed.step(-1);
    expect(ed.state).toMatchObject({ mode: "edit", name: "alpha", info: "first" });

    await ed.step(-1);
    expect(ed.selected?.id).toBe("a");
  });

  it("step is ignored while a new note is being created", async () => {
    ed.startNew();
    await ed.step(1);

    expect(ed.state.mode).toBe("new");
    expect(ed.selected?.id).toBe("a");
  });

  it("cancel in new mode returns to the previous note", async () => {
    await ed.select("b");
    ed.startNew();
    ed.cancel();

    expect(ed.state).toMatchObject({ mode: "edit", name: "beta" });
  });

  it("cancel in edit mode restores the saved fields", async () => {
    await ed.select("a");
    ed.name = "renamed";
    ed.cancel();

    expect(ed.state).toMatchObject({ mode: "edit", name: "alpha", info: "first" });
  });

  it("a failed apply shows the error and keeps the edit", async () => {
    await ed.select("a");
    api.Save = async () => {
      throw new Error('{"code":"validation","message":"invalid name: too long","field":"name"}');
    };
    ed.name = "x";
    await ed.apply();

    expect(d.shown).toEqual(["error: invalid name: too long"]);
    expect(ed.state).toMatchObject({ name: "x", savedName: "alpha" });
  });

  it("treats null lists from Go as empty", async () => {
    api.List = async () => null;
    api.Versions = async () => null;
    await ed.load();
    expect(ed.notes).toEqual([]);
    expect(ed.state.mode).toBe("view");
    expect(ed.selected).toBeUndefined();
  });

  it("load after a Restore drops the edit and keeps an existing selection", async () => {
    await ed.select("b");
    ed.info = "unsaved";
    api.notes[1] = { ...api.notes[1], info: "restored" };
    await ed.load();

    expect(ed.state).toMatchObject({ mode: "edit", info: "restored", savedInfo: "restored" });
  });

  it("ignores a second Apply while the first Create runs", async () => {
    const releases: (() => void)[] = [];
    const create = api.Create.bind(api);
    api.Create = async (name, info) => {
      await new Promise<void>((r) => releases.push(r));
      return create(name, info);
    };
    ed.startNew();
    ed.name = "gamma";
    ed.info = "third";
    const first = ed.apply();
    const second = ed.apply();
    releases.forEach((r) => r());
    await Promise.all([first, second]);

    expect(api.calls.filter((c) => c.startsWith("Create"))).toEqual(["Create gamma"]);
    expect(api.notes.filter((n) => n.name === "gamma")).toHaveLength(1);
  });

  it("locks the editor while Save runs", async () => {
    await ed.select("a");
    let release!: () => void;
    const save = api.Save.bind(api);
    api.Save = async (id, name, info) => {
      await new Promise<void>((r) => (release = r));
      return save(id, name, info);
    };
    ed.info = "first edit";
    const applying = ed.apply();

    expect(ed.locked).toBe(true);
    expect(Object.values(ed.buttons).some(Boolean)).toBe(false);
    await ed.select("b");
    ed.startNew();
    ed.cancel();
    ed.useVersion("old");
    expect(ed.state).toMatchObject({ mode: "edit", name: "alpha", info: "first edit" });

    release();
    await applying;
    expect(ed.locked).toBe(false);
    expect(ed.state).toMatchObject({ mode: "edit", name: "alpha", info: "first edit", savedInfo: "first edit" });
  });

  it("locks the editor while Delete runs", async () => {
    await ed.select("a");
    let release!: () => void;
    const del = api.Delete.bind(api);
    api.Delete = async (id) => {
      await new Promise<void>((r) => (release = r));
      return del(id);
    };
    const removing = ed.remove();
    await Promise.resolve();
    await Promise.resolve();

    expect(ed.locked).toBe(true);
    await ed.select("b");
    expect(ed.selected?.id).toBe("a");

    release();
    await removing;
    expect(ed.busy).toBe(false);
    expect(ed.selected?.id).toBe("b");
    expect(ed.notes.map((n) => n.id)).toEqual(["b"]);
  });
});
