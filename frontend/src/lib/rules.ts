// Button rules of the note editor (spec §7.1).
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

export function buttons(s: EditorState): Buttons {
  const creating = s.mode === "new";
  const changed = s.mode !== "view" && (s.name !== s.savedName || normalizeText(s.info) !== normalizeText(s.savedInfo));
  const filled = s.name.trim() !== "" && normalizeText(s.info) !== "";

  return {
    apply: filled && (creating || changed),
    cancel: creating || changed,
    new: !creating,
    delete: s.mode === "edit",
    list: !creating && s.hasNotes,
    history: s.hasVersions && s.mode === "edit",
  };
}
