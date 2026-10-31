import { describe, expect, it } from "vitest";
import { buttons, settingsButtons, type EditorState } from "./rules";

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
