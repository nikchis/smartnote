<script lang="ts">
  // Main lists the notes and edits the selected one, laid out as v1 UMain:
  // the list on the left, the editor group on the right and the status bar
  // with Save/Restore, the credits, the password key and Settings.
  import { tick } from "svelte";
  import historyIcon from "../assets/icons/history.png";
  import newIcon from "../assets/icons/new.png";
  import passwordIcon from "../assets/icons/password.png";
  import removeIcon from "../assets/icons/remove.png";
  import restoreIcon from "../assets/icons/restore.png";
  import saveIcon from "../assets/icons/save.png";
  import { Events } from "@wailsio/runtime";
  import SearchBar from "../components/SearchBar.svelte";
  import { NotesService, SearchService, type SearchHitDTO, type StateDTO } from "../lib/api";
  import { dialogs } from "../lib/dialogs.svelte";
  import { Editor, type Place } from "../lib/editor.svelte";
  import { scrollTopFor } from "../lib/jump";
  import { isFindShortcut, SearchBox } from "../lib/search.svelte";
  import { LIMITS, utf16Len } from "../lib/text";
  import ChangePassword from "./ChangePassword.svelte";
  import History from "./History.svelte";

  let {
    sync,
    onSettings,
    onBackup,
    onRestore,
  }: { sync: StateDTO; onSettings: () => void; onBackup: () => void; onRestore: () => void } = $props();

  const search = new SearchBox(SearchService);
  const editor = new Editor(NotesService, dialogs, search);
  const b = $derived(editor.buttons);
  let showHistory = $state(false);
  let changingPassword = $state(false);
  let list: HTMLElement | undefined = $state();
  let searchBar: ReturnType<typeof SearchBar> | undefined = $state();
  let nameField: HTMLInputElement | undefined = $state();
  let textField: HTMLTextAreaElement | undefined = $state();
  const mac = navigator.userAgent.includes("Mac");

  /** reload reads the notes again, after a Restore; an unsaved edit is lost. */
  export function reload(): Promise<void> {
    return editor.load();
  }

  /**
   * focusSelected puts the focus on the selected row. While a modal covers the
   * window the row is inert; the modal moves the focus there when it closes.
   */
  async function focusSelected(): Promise<void> {
    await tick();
    const row = list?.querySelector<HTMLElement>("[data-focus-home]");
    if (row && !row.closest("[inert]")) row.focus();
  }

  $effect(() => {
    void editor.load().then(focusSelected);
  });

  // The status of the search index: subscribe first, then read the last one,
  // so that no status sent in between is lost; seed keeps a newer event.
  $effect(() => {
    const off = Events.On("search:status", (ev) => search.follow(ev.data));
    SearchService.Status().then(
      (s) => search.seed(s),
      // Status does not fail in Go; until an event comes the status line is empty.
      () => undefined,
    );
    return off;
  });

  /** onWindowKey puts the focus on the search field with Ctrl+F (Cmd+F on macOS), unless a modal is open. */
  function onWindowKey(e: KeyboardEvent): void {
    if (!isFindShortcut(e, mac) || list?.closest("[inert]")) return;
    e.preventDefault();
    searchBar?.focus();
  }

  const nameOf = (id: string) => editor.notes.find((n) => n.id === id)?.name ?? "";

  /** jumps counts the calls of openHit: a jump that a newer one replaced does not touch the fields. */
  let jumps = 0;

  /** openHit jumps to a hit and selects its place (search spec §9.2). */
  async function openHit(hit: SearchHitDTO): Promise<void> {
    const mine = ++jumps;
    const jump = await editor.openHit(hit);
    // The fields show the note and the Discard question is gone after the tick.
    await tick();
    if (mine !== jumps) return;
    switch (jump) {
      case "selected":
        if (editor.place) selectPlace(editor.place);
        break;
      case "unselected":
        if (textField) selectPlace({ field: "info", start: 0, end: 0 });
        break;
      case "gone":
        searchBar?.focus();
        break;
      case "cancelled":
        break;
    }
  }

  /**
   * selectPlace selects a range of the name or of the text; the text is
   * scrolled by the mirror so that the range is in the middle.
   */
  function selectPlace(p: Place): void {
    const field = p.field === "name" ? nameField : textField;
    if (!field) return;
    field.focus({ preventScroll: true });
    field.setSelectionRange(p.start, p.end);
    if (field instanceof HTMLTextAreaElement) field.scrollTop = scrollTopFor(field, p.start);
  }

  /** onListKey moves through the list with the arrow keys, as in the v1 grid. */
  function onListKey(e: KeyboardEvent): void {
    const delta = e.key === "ArrowDown" ? 1 : e.key === "ArrowUp" ? -1 : 0;
    if (delta === 0) return;
    e.preventDefault();
    void editor.step(delta).then(focusSelected);
  }

  const updated = $derived(editor.mode === "edit" && editor.selected ? new Date(editor.selected.updated).toLocaleString() : "");
</script>

<svelte:window onkeydown={onWindowKey} />

<div class="main">
  <div class="find">
    <SearchBar bind:this={searchBar} box={search} {nameOf} onOpen={(hit) => void openHit(hit)} />
  </div>
  <div class="content">
    <div class="list" bind:this={list}>
      <div class="head">Name</div>
      <ul aria-label="Notes">
        {#each editor.notes as note (note.id)}
          {@const active = editor.selected?.id === note.id}
          <li>
            <button class:active data-focus-home={active ? "" : undefined} disabled={!b.list} onclick={() => editor.select(note.id)} onkeydown={onListKey}>
              {note.name}
            </button>
          </li>
        {/each}
      </ul>
    </div>

    <section class="editor">
      <div class="fields">
        <label class="name">
          Name:
          <input bind:this={nameField} bind:value={editor.name} maxlength={LIMITS.name} disabled={editor.locked} />
        </label>
        <label class="muted">
          Updated:
          <input class="updated" value={updated} readonly tabindex="-1" />
        </label>
        <label class="muted">
          Length:
          <input class="length" value="{utf16Len(editor.info)}/{LIMITS.info}" readonly tabindex="-1" />
        </label>
        <button class="history" onclick={() => (showHistory = true)} disabled={!b.history}>
          <img src={historyIcon} alt="" />History
        </button>
      </div>
      <textarea bind:this={textField} bind:value={editor.info} maxlength={LIMITS.info} disabled={editor.locked} aria-label="Text"></textarea>
      <div class="actions">
        <button class="apply" type="submit" onclick={() => editor.apply()} disabled={!b.apply}>Apply changes</button>
        <div class="center">
          <button title="Add a new note" onclick={() => editor.startNew()} disabled={!b.new}>
            <img src={newIcon} alt="" />New
          </button>
          <button title="Remove current note" onclick={() => editor.remove()} disabled={!b.delete}>
            <img src={removeIcon} alt="" />Remove
          </button>
        </div>
        <button class="cancel" onclick={() => editor.cancel()} disabled={!b.cancel}>Cancel changes</button>
      </div>
    </section>
  </div>

  <footer class="bar">
    <button title="Save data to remote storage" onclick={onBackup} disabled={!sync.connected || !sync.dirty || sync.busy}>
      <img class="mono" src={saveIcon} alt="" />Save
    </button>
    <button title="Restore data from remote storage" onclick={onRestore} disabled={!sync.connected || !sync.restoreAvailable || sync.busy}>
      <img class="mono" src={restoreIcon} alt="" />Restore
    </button>
    <span class="spacer"></span>
    <span class="credit">MIT License</span>
    <span class="credit">© Nikita Chisnikov, 2015–2026</span>
    <span class="credit">github.com/nikchis/smartnote</span>
    <span class="spacer"></span>
    <button title="Set new password" aria-label="Set new password" onclick={() => (changingPassword = true)}>
      <img src={passwordIcon} alt="" />
    </button>
    <button title="Connection settings" onclick={onSettings}>Settings</button>
  </footer>
</div>

{#if showHistory}
  <History
    versions={editor.versions}
    onUse={(info) => {
      editor.useVersion(info);
      showHistory = false;
    }}
    onClose={() => (showHistory = false)}
  />
{/if}

{#if changingPassword}
  <ChangePassword onClose={() => (changingPassword = false)} />
{/if}

<style>
  .main {
    width: 100%;
    height: 100%;
    display: grid;
    grid-template-rows: auto 1fr auto;
  }
  .find {
    padding: 0.75rem 0.75rem 0;
  }
  .content {
    display: grid;
    grid-template-columns: 13.6rem 1fr;
    gap: 0.75rem;
    min-height: 0;
    padding: 0.75rem;
  }
  button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.4rem;
    white-space: nowrap;
  }
  img {
    width: 16px;
    height: 16px;
    flex: none;
  }
  /* The v1 Save and Restore glyphs are black; the dark skin shows them light. */
  img.mono {
    filter: invert(1);
  }

  /* The list: one "Name" column with striped rows, as the v1 grid. */
  .list {
    display: flex;
    flex-direction: column;
    min-height: 0;
    border: 1px solid var(--border);
    background: var(--row);
  }
  .head {
    padding: 0.2rem 0.4rem;
    border-bottom: 1px solid var(--border);
    background: var(--border);
  }
  .list ul {
    flex: 1;
    margin: 0;
    padding: 0;
    overflow: auto;
    list-style: none;
  }
  .list button {
    display: block;
    width: 100%;
    padding: 0.15rem 0.4rem;
    overflow: hidden;
    text-overflow: ellipsis;
    border: none;
    border-bottom: 1px solid var(--border);
    border-radius: 0;
    background: none;
    text-align: left;
  }
  .list li:nth-child(odd) button {
    background: var(--stripe);
  }
  .list button:disabled {
    opacity: 1;
    color: var(--muted);
  }
  /* As in the v1 grid, the selected row is framed with dots, not filled. */
  .list li button.active,
  .list button:focus-visible {
    outline: 1px dotted var(--text);
    outline-offset: -2px;
  }

  /* The editor group: fields, text, Apply | New Remove | Cancel. */
  .editor {
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
    min-width: 0;
    min-height: 0;
    padding: 0.6rem;
    border: 1px solid var(--border);
  }
  .fields {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }
  .fields label {
    flex-direction: row;
    align-items: center;
    gap: 0.4rem;
    white-space: nowrap;
  }
  .fields .name {
    flex: 1;
    min-width: 0;
    color: inherit;
  }
  .name input {
    flex: 1;
    min-width: 0;
  }
  .updated,
  .length {
    color: var(--muted);
    text-align: center;
  }
  .updated {
    width: 11rem;
  }
  .length {
    width: 6rem;
  }
  textarea {
    flex: 1;
    min-height: 0;
    resize: none;
    font: var(--note-font);
  }
  .actions {
    display: grid;
    grid-template-columns: 1fr auto 1fr;
    align-items: center;
    gap: 0.5rem;
  }
  .apply {
    justify-self: start;
  }
  .center {
    display: flex;
    gap: 0.5rem;
  }
  .cancel {
    justify-self: end;
  }

  /* The status bar. */
  .bar {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    padding: 0.2rem 0.3rem;
    border-top: 1px solid var(--border);
    background: var(--surface);
    font-size: 0.85rem;
  }
  .bar button {
    padding: 0.2rem 0.5rem;
  }
  .credit {
    padding: 0 0.75rem;
    color: var(--muted);
  }
  .credit + .credit {
    border-left: 1px solid var(--border);
  }
  .spacer {
    flex: 1;
  }
</style>
