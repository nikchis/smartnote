// State and actions of the note editor on the Main screen (spec §7, §7.1)
// and the jump to a search hit (search spec §9.2, §9.3).
import type { NoteDTO, SearchHitDTO, VersionDTO } from "./api";
import type { Dialogs } from "./dialogs.svelte";
import { parseError } from "./errors";
import { textMatches } from "./jump";
import { buttons, hasDraft, type Buttons, type EditorState } from "./rules";

const NO_BUTTONS: Buttons = { apply: false, cancel: false, new: false, delete: false, list: false, history: false };

/** NotesApi is the part of NotesService the editor uses. */
export type NotesApi = {
  List(): Promise<NoteDTO[] | null>;
  Create(name: string, info: string): Promise<NoteDTO>;
  Save(id: string, name: string, info: string): Promise<NoteDTO>;
  Delete(id: string): Promise<void>;
  Versions(id: string): Promise<VersionDTO[] | null>;
};

/** Research repeats the current search; SearchBox is one. */
export type Research = { again(): Promise<SearchHitDTO[]> };

/** Place is the range openHit found, for the screen to select: in the name or in the text. */
export type Place = { field: "name" | "info"; start: number; end: number };

/** Jump is how openHit ended: the place is selected, the note is open without a selection, the draft was kept, or the note is gone. */
export type Jump = "selected" | "unselected" | "cancelled" | "gone";

const DISCARD = { message: "Discard unsaved changes?", labels: { yes: "Discard", no: "Cancel" } } as const;

export class Editor {
  notes: NoteDTO[] = $state([]);
  /** The selected note, also while a new one is being created. */
  selected: NoteDTO | undefined = $state();
  versions: VersionDTO[] = $state([]);
  mode: EditorState["mode"] = $state("view");
  name = $state("");
  info = $state("");
  /** busy is set while Apply or Delete waits for Go; the editor is locked meanwhile. */
  busy = $state(false);

  readonly state: EditorState = $derived({
    mode: this.mode,
    name: this.name,
    info: this.info,
    savedName: this.mode === "edit" ? (this.selected?.name ?? "") : "",
    savedInfo: this.mode === "edit" ? (this.selected?.info ?? "") : "",
    hasVersions: this.versions.length > 0,
    hasNotes: this.notes.length > 0,
  });
  readonly buttons: Buttons = $derived(this.busy ? NO_BUTTONS : buttons(this.state));
  /** locked disables the fields: nothing is selected, or an action runs. */
  readonly locked: boolean = $derived(this.mode === "view" || this.busy);

  constructor(
    private readonly api: NotesApi,
    private readonly dialogs: Dialogs,
    private readonly search: Research,
  ) {}

  /** place is the range the last openHit selected, undefined when it selected none. */
  place: Place | undefined;

  /**
   * load reads the notes, for the first time or after a Restore: an unsaved
   * edit is lost, the selected note stays selected if it still exists,
   * otherwise the first note is selected.
   */
  async load(): Promise<void> {
    try {
      this.notes = (await this.api.List()) ?? [];
    } catch (e) {
      await this.dialogs.error(parseError(e).message);
      return;
    }
    // Without a selection to keep, the first note is shown, as the v1 grid focused it.
    const keep = (this.selected && this.notes.find((n) => n.id === this.selected?.id)) || this.notes[0];
    if (keep) {
      await this.show(keep);
    } else {
      this.clear();
    }
  }

  /**
   * select shows note id, picked in the list. With unsaved changes it asks
   * first (search spec §9.3, a change from v1); the selected note itself is
   * left as it is. It is false when the note is not shown: the user kept the
   * draft, a new note is being created (the list is off then) or an action runs.
   */
  async select(id: string): Promise<boolean> {
    if (this.mode === "new" || this.busy) return false;
    const note = this.notes.find((n) => n.id === id);
    if (!note) return false;
    if (note.id === this.selected?.id) return true;
    if (!(await this.discardDraft())) return false;
    await this.show(note);
    return true;
  }

  /**
   * openHit opens the note of a search hit and finds the place to select
   * (search spec §9.2). A draft, also of the same note or of a new one, is
   * replaced only after Discard. When the text of the hit is no longer at its
   * place, the search runs once more and the hit of the note with the nearest
   * line is taken; if that does not match either, the note stays open without
   * a selection. A note that is gone is not opened and the hits are renewed.
   */
  async openHit(hit: SearchHitDTO): Promise<Jump> {
    this.place = undefined;
    if (this.busy) return "cancelled";
    const note = this.notes.find((n) => n.id === hit.noteId);
    if (!note) {
      await this.search.again();
      return "gone";
    }
    // The open note without a draft stays as it is; anything else shows the saved fields.
    const shown = this.mode === "edit" && this.selected?.id === note.id && !hasDraft(this.state);
    if (!(await this.discardDraft())) return "cancelled";
    if (!shown) await this.show(note);

    const found = textMatches(hit, this.name, this.info) ? hit : await this.findAgain(hit);
    if (!found) return "unselected";
    this.place = { field: found.line === 0 ? "name" : "info", start: found.start, end: found.end };
    return "selected";
  }

  /**
   * step selects the note delta rows away from the selected one, as the
   * arrow keys moved through the v1 grid; it stops at the ends of the list.
   */
  async step(delta: number): Promise<void> {
    const at = this.notes.findIndex((n) => n.id === this.selected?.id);
    const next = this.notes[Math.min(Math.max(at + delta, 0), this.notes.length - 1)];
    if (next && next.id !== this.selected?.id) await this.select(next.id);
  }

  startNew(): void {
    if (this.busy) return;
    this.mode = "new";
    this.name = "";
    this.info = "";
  }

  /** cancel leaves the new note, or restores the selected note's saved fields. */
  cancel(): void {
    if (this.busy) return;
    if (this.selected) {
      this.mode = "edit";
      this.name = this.selected.name;
      this.info = this.selected.info;
    } else {
      this.clear();
    }
  }

  async apply(): Promise<void> {
    if (!this.buttons.apply) return;
    const creating = this.mode === "new";
    const id = this.selected?.id;
    if (!creating && !id) return;
    await this.exclusive(async () => {
      const note = creating ? await this.api.Create(this.name, this.info) : await this.api.Save(id!, this.name, this.info);
      await this.reload(note);
    }, creating ? "Note has been successfully added." : "Note has been successfully updated.");
  }

  async remove(): Promise<void> {
    const note = this.selected;
    if (!note || !this.buttons.delete) return;
    if (!(await this.dialogs.ask("Are you sure you want to remove this note?"))) return;
    await this.exclusive(async () => {
      await this.api.Delete(note.id);
      this.selected = undefined;
      await this.load();
    }, "Note has been successfully removed.");
  }

  /** useVersion puts an old text into the editor; only Apply saves it. */
  useVersion(info: string): void {
    if (this.busy) return;
    this.info = info;
  }

  /** discardDraft asks before unsaved input is replaced; true when there is none or the user discards it. */
  private async discardDraft(): Promise<boolean> {
    return !hasDraft(this.state) || this.dialogs.ask(DISCARD.message, DISCARD.labels);
  }

  /** findAgain searches again for a hit of the same note, the one with the nearest line, if its text matches. */
  private async findAgain(hit: SearchHitDTO): Promise<SearchHitDTO | undefined> {
    const same = (await this.search.again()).filter((h) => h.noteId === hit.noteId);
    const nearest = same.reduce<SearchHitDTO | undefined>(
      (best, h) => (!best || Math.abs(h.line - hit.line) < Math.abs(best.line - hit.line) ? h : best),
      undefined,
    );
    return nearest && textMatches(nearest, this.name, this.info) ? nearest : undefined;
  }

  /**
   * exclusive runs call with the editor locked, so that nothing typed or
   * selected meanwhile is replaced by the answer, then shows done or the error.
   */
  private async exclusive(call: () => Promise<void>, done: string): Promise<void> {
    if (this.busy) return;
    this.busy = true;
    let message: string | undefined;
    try {
      await call();
    } catch (e) {
      message = parseError(e).message;
    } finally {
      this.busy = false;
    }
    await (message === undefined ? this.dialogs.info(done) : this.dialogs.error(message));
  }

  private async reload(saved: NoteDTO): Promise<void> {
    this.selected = saved;
    await this.load();
  }

  private async show(note: NoteDTO): Promise<void> {
    this.selected = note;
    this.mode = "edit";
    this.name = note.name;
    this.info = note.info;
    this.versions = [];
    try {
      const versions = await this.api.Versions(note.id);
      if (this.selected?.id === note.id) this.versions = versions ?? [];
    } catch (e) {
      await this.dialogs.error(parseError(e).message);
    }
  }

  private clear(): void {
    this.selected = undefined;
    this.versions = [];
    this.mode = "view";
    this.name = "";
    this.info = "";
  }
}
