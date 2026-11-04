import { describe, expect, it } from "vitest";
import { buttons, hasDraft, settingsButtons, type EditorState } from "./rules";

const base: EditorState = {
  mode: "edit", name: "n", info: "i", savedName: "n", savedInfo: "i", hasVersions: false, hasNotes: true,
};

describe("buttons", () => {
  it("new note with empty fields: only Cancel", () => {
    const b = buttons({ ...base, mode: "new", name: "", info: "", savedName: "", savedInfo: "" });
    expect(b).toMatchObject({ cancel: true, apply: false, new: false, list: false, delete: false });
  });

  it("new note with one field filled: Cancel, no Apply", () => {
    const b = buttons({ ...base, mode: "new", name: "n", info: "", savedName: "", savedInfo: "" });
    expect(b).toMatchObject({ cancel: true, apply: false });
  });

  it("new note with both fields: Apply", () => {
    const b = buttons({ ...base, mode: "new", name: "n", info: "i", savedName: "", savedInfo: "" });
    expect(b).toMatchObject({ cancel: true, apply: true });
  });

  it("edit without changes: neither Apply nor Cancel", () => {
    expect(buttons(base)).toMatchObject({ apply: false, cancel: false, new: true, delete: true, list: true });
  });

  it("edit with a changed name: Apply and Cancel", () => {
    expect(buttons({ ...base, name: "m" })).toMatchObject({ apply: true, cancel: true });
  });

  it("edit with a changed text: Apply and Cancel", () => {
    expect(buttons({ ...base, info: "j" })).toMatchObject({ apply: true, cancel: true });
  });

  it("empty or blank name with changes: Cancel without Apply", () => {
    expect(buttons({ ...base, name: "" })).toMatchObject({ apply: false, cancel: true });
    expect(buttons({ ...base, name: "  " })).toMatchObject({ apply: false, cancel: true });
  });

  it("empty text with changes: Cancel without Apply", () => {
    expect(buttons({ ...base, info: "" })).toMatchObject({ apply: false, cancel: true });
  });

  it("CRLF against the saved LF text is no change", () => {
    expect(buttons({ ...base, info: "a\r\nb", savedInfo: "a\nb" })).toMatchObject({ apply: false, cancel: false });
  });

  it.each([
    { mode: "edit", hasVersions: true, history: true },
    { mode: "edit", hasVersions: false, history: false },
    { mode: "new", hasVersions: true, history: false },
  ] as const)("history in $mode with versions=$hasVersions is $history", ({ mode, hasVersions, history }) => {
    expect(buttons({ ...base, mode, hasVersions }).history).toBe(history);
  });

  it("nothing selected: New and the list only", () => {
    const b = buttons({ ...base, mode: "view", name: "", info: "", savedName: "", savedInfo: "" });
    expect(b).toEqual({ apply: false, cancel: false, new: true, delete: false, list: true, history: false });
  });

  it("no notes: the list is inactive", () => {
    expect(buttons({ ...base, mode: "view", hasNotes: false }).list).toBe(false);
  });
});

describe("hasDraft", () => {
  const blank = { name: "", info: "", savedName: "", savedInfo: "" };

  it("view has no draft", () => {
    expect(hasDraft({ ...base, ...blank, mode: "view" })).toBe(false);
  });

  it("edit without changes has no draft", () => {
    expect(hasDraft(base)).toBe(false);
  });

  it("edit with a changed name or text has a draft", () => {
    expect(hasDraft({ ...base, name: "m" })).toBe(true);
    expect(hasDraft({ ...base, info: "j" })).toBe(true);
  });

  it("edit with CRLF against the saved LF text has no draft", () => {
    expect(hasDraft({ ...base, info: "a\r\nb", savedInfo: "a\nb" })).toBe(false);
  });

  it("an empty new note has no draft", () => {
    expect(hasDraft({ ...base, ...blank, mode: "new" })).toBe(false);
  });

  it("a new note with a blank name and no text has no draft", () => {
    expect(hasDraft({ ...base, ...blank, mode: "new", name: "   " })).toBe(false);
  });

  it("a new note with a text has a draft", () => {
    expect(hasDraft({ ...base, ...blank, mode: "new", info: "text" })).toBe(true);
  });

  it("a new note with a name has a draft", () => {
    expect(hasDraft({ ...base, ...blank, mode: "new", name: "n" })).toBe(true);
  });
});

describe("settingsButtons", () => {
  it("keeps Test, OK and every key action inactive while the settings load", () => {
    expect(settingsButtons(false, undefined)).toEqual({ test: false, save: false, generate: false, export: false, import: false });
  });

  it("keeps the key actions inactive until it is known whether a key exists", () => {
    expect(settingsButtons(true, undefined)).toEqual({ test: true, save: true, generate: false, export: false, import: false });
  });

  it("allows Generate and Import without a key", () => {
    expect(settingsButtons(true, false)).toEqual({ test: true, save: true, generate: true, export: false, import: true });
  });

  it("allows Export with a key", () => {
    expect(settingsButtons(true, true)).toEqual({ test: true, save: true, generate: true, export: true, import: true });
  });
});
