// State and actions of the note editor on the Main screen (spec §7, §7.1).
import type { NoteDTO, VersionDTO } from "./api";
import type { Dialogs } from "./dialogs.svelte";
import { parseError } from "./errors";
import { buttons, type Buttons, type EditorState } from "./rules";

/** NotesApi is the part of NotesService the editor uses. */
export type NotesApi = {
  List(): Promise<NoteDTO[] | null>;
  Create(name: string, info: string): Promise<NoteDTO>;
  Save(id: string, name: string, info: string): Promise<NoteDTO>;
  Delete(id: string): Promise<void>;
  Versions(id: string): Promise<VersionDTO[] | null>;
};

export class Editor {
  notes: NoteDTO[] = $state([]);
  /** The selected note, also while a new one is being created. */
  selected: NoteDTO | undefined = $state();
  versions: VersionDTO[] = $state([]);
  mode: EditorState["mode"] = $state("view");
  name = $state("");
  info = $state("");

  readonly state: EditorState = $derived({
    mode: this.mode,
    name: this.name,
    info: this.info,
    savedName: this.mode === "edit" ? (this.selected?.name ?? "") : "",
    savedInfo: this.mode === "edit" ? (this.selected?.info ?? "") : "",
    hasVersions: this.versions.length > 0,
    hasNotes: this.notes.length > 0,
  });
  readonly buttons: Buttons = $derived(buttons(this.state));

  constructor(
    private readonly api: NotesApi,
    private readonly dialogs: Dialogs,
  ) {}

  /**
   * load reads the notes, for the first time or after a Restore: an unsaved
   * edit is lost, the selected note stays selected if it still exists.
   */
  async load(): Promise<void> {
    try {
      this.notes = (await this.api.List()) ?? [];
    } catch (e) {
      await this.dialogs.error(parseError(e).message);
      return;
    }
    const keep = this.selected && this.notes.find((n) => n.id === this.selected?.id);
    if (keep) {
      await this.show(keep);
    } else {
      this.clear();
    }
  }

  /** select shows note id; an unsaved edit is lost without asking, as in v1. */
  async select(id: string): Promise<void> {
    if (this.mode === "new") return;
    const note = this.notes.find((n) => n.id === id);
    if (note) await this.show(note);
  }

  startNew(): void {
    this.mode = "new";
    this.name = "";
    this.info = "";
  }

  /** cancel leaves the new note, or restores the selected note's saved fields. */
  cancel(): void {
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
    try {
      if (this.mode === "new") {
        const note = await this.api.Create(this.name, this.info);
        await this.reload(note);
        await this.dialogs.info("Note has been successfully added.");
      } else if (this.selected) {
        const note = await this.api.Save(this.selected.id, this.name, this.info);
        await this.reload(note);
        await this.dialogs.info("Note has been successfully updated.");
      }
    } catch (e) {
      await this.dialogs.error(parseError(e).message);
    }
  }

  async remove(): Promise<void> {
    const note = this.selected;
    if (!note || !this.buttons.delete) return;
    if (!(await this.dialogs.ask("Are you sure you want to remove this note?"))) return;
    try {
      await this.api.Delete(note.id);
      this.selected = undefined;
      await this.load();
      await this.dialogs.info("Note has been successfully removed.");
    } catch (e) {
      await this.dialogs.error(parseError(e).message);
    }
  }

  /** useVersion puts an old text into the editor; only Apply saves it. */
  useVersion(info: string): void {
    this.info = info;
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
