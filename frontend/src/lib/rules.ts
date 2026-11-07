// Button rules of the note editor (spec §7.1) and of the client key in Settings.
import { normalizeText } from "./text";

/**
 * EditorState is what the buttons depend on. view: no note is selected;
 * new: a note is being created; edit: an existing note is selected.
 */
export type EditorState = {
  mode: "view" | "new" | "edit";
  name: string;
  info: string;
  savedName: string;
  savedInfo: string;
  hasVersions: boolean;
  hasNotes: boolean;
};

export type Buttons = { apply: boolean; cancel: boolean; new: boolean; delete: boolean; list: boolean; history: boolean };

/** changed tells whether the fields differ from the saved ones; in new mode the saved ones are empty. */
function changed(s: EditorState): boolean {
  return s.mode !== "view" && (s.name !== s.savedName || normalizeText(s.info) !== normalizeText(s.savedInfo));
}

export function buttons(s: EditorState): Buttons {
  const creating = s.mode === "new";
  const filled = s.name.trim() !== "" && normalizeText(s.info) !== "";

  return {
    apply: filled && (creating || changed(s)),
    cancel: creating || changed(s),
    new: !creating,
    delete: s.mode === "edit",
    list: !creating && s.hasNotes,
    history: s.hasVersions && s.mode === "edit",
  };
}

/**
 * hasDraft tells whether the editor holds unsaved input that a jump to a note
 * would lose (search spec §9.2): changed fields of a note, or a new note with
 * a name or a text. A blank name alone is no draft.
 */
export function hasDraft(s: EditorState): boolean {
  switch (s.mode) {
    case "edit":
      return changed(s);
    case "new":
      return s.name.trim() !== "" || normalizeText(s.info) !== "";
    case "view":
      return false;
  }
}

export type SettingsButtons = { test: boolean; save: boolean; generate: boolean; export: boolean; import: boolean };

/**
 * settingsButtons are the buttons of Settings that act on what was loaded.
 * Until the settings load, OK would save the empty form; until hasKey loads,
 * Generate could replace a key without the question. Without a storage there
 * is nothing to test; while a Yandex sign-in runs neither Test nor OK may start
 * (spec §8).
 */
export function settingsButtons(loaded: boolean, hasKey: boolean | undefined, kind: string, signingIn: boolean): SettingsButtons {
  const known = hasKey !== undefined;
  const ready = loaded && !signingIn;
  return { test: ready && kind !== "none", save: ready, generate: known, export: hasKey === true, import: known };
}
