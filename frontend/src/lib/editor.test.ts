import { beforeEach, describe, expect, it } from "vitest";
import type { NoteDTO, VersionDTO } from "./api";
import type { Dialogs } from "./dialogs.svelte";
import { Editor, type NotesApi } from "./editor.svelte";

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
  answer = true;
  async info(msg: string) {
    this.shown.push(`info: ${msg}`);
  }
  async error(msg: string) {
    this.shown.push(`error: ${msg}`);
  }
  async ask(msg: string) {
    this.shown.push(`ask: ${msg}`);
    return this.answer;
  }
}

let api: FakeNotes;
let d: FakeDialogs;
let ed: Editor;

beforeEach(async () => {
  api = new FakeNotes();
  api.notes = [
    { id: "a", name: "alpha", info: "first", updated: 1 },
    { id: "b", name: "beta", info: "second", updated: 2 },
  ];
  d = new FakeDialogs();
  ed = new Editor(api, d);
  await ed.load();
});

describe("Editor", () => {
  it("starts with nothing selected", () => {
    expect(ed.state.mode).toBe("view");
    expect(ed.notes.map((n) => n.name)).toEqual(["alpha", "beta"]);
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

  it("remove on Yes deletes and clears the selection", async () => {
    await ed.select("a");
    await ed.remove();

    expect(api.calls).toContain("Delete a");
    expect(d.shown).toEqual(["ask: Are you sure you want to remove this note?", "info: Note has been successfully removed."]);
    expect(ed.state.mode).toBe("view");
    expect(ed.notes.map((n) => n.id)).toEqual(["b"]);
  });

  it("selecting another note drops an unsaved edit without asking", async () => {
    await ed.select("a");
    ed.info = "unsaved";
    await ed.select("b");

    expect(d.shown).toEqual([]);
    expect(ed.state).toMatchObject({ mode: "edit", name: "beta", info: "second" });
    expect(api.notes.find((n) => n.id === "a")?.info).toBe("first");
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
  });

  it("load after a Restore drops the edit and keeps an existing selection", async () => {
    await ed.select("b");
    ed.info = "unsaved";
    api.notes[1] = { ...api.notes[1], info: "restored" };
    await ed.load();

    expect(ed.state).toMatchObject({ mode: "edit", info: "restored", savedInfo: "restored" });
  });
});
