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
  import shieldIcon from "../assets/icons/shield.png";
  import { Events } from "@wailsio/runtime";
  import LanguageToggle from "../components/LanguageToggle.svelte";
  import PrivacyPolicy from "../components/PrivacyPolicy.svelte";
  import SearchBar from "../components/SearchBar.svelte";
  import { NotesService, SearchService, type SearchHitDTO, type StateDTO } from "../lib/api";
  import { dialogs } from "../lib/dialogs.svelte";
  import { Editor, type Place } from "../lib/editor.svelte";
  import { i18n } from "../lib/i18n.svelte";
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
  let privacyOpen = $state(false);
  let list: HTMLElement | undefined = $state();
  let searchBar: ReturnType<typeof SearchBar> | undefined = $state();
  let nameField: HTMLInputElement | undefined = $state();
  let textField: HTMLTextAreaElement | undefined = $state();
  const mac = navigator.userAgent.includes("Mac");

  /** reload reads the notes again, after a Restore; ask discardDraft before it, or an unsaved edit is lost. */
  export function reload(): Promise<void> {
    return editor.load();
  }

  /** discardDraft asks before a Restore or closing loses an unsaved edit; true when there is none or it is discarded. */
  export function discardDraft(): Promise<boolean> {
    return editor.discardDraft();
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

  const updated = $derived(editor.mode === "edit" && editor.selected ? new Date(editor.selected.updated).toLocaleString(i18n.lang) : "");
</script>

<svelte:window onkeydown={onWindowKey} />

<div class="main">
  <div class="find">
    <div class="notes">
      <button title={i18n.t("main.newTitle")} onclick={() => void editor.startNew()} disabled={!b.new}>
        <img src={newIcon} alt="" />{i18n.t("main.new")}
      </button>
      <button title={i18n.t("main.removeTitle")} onclick={() => editor.remove()} disabled={!b.delete}>
        <img src={removeIcon} alt="" />{i18n.t("main.remove")}
      </button>
    </div>
    <SearchBar bind:this={searchBar} box={search} {nameOf} onOpen={(hit) => void openHit(hit)} />
    <!-- The buttons at the right end of the row, the language first (spec §16.4). -->
    <div class="tools">
      <LanguageToggle />
      <button type="button" title={i18n.t("privacy.title")} aria-label={i18n.t("privacy.title")} onclick={() => (privacyOpen = true)}>
        <img class="mono" src={shieldIcon} alt="" />
      </button>
    </div>
  </div>
  <div class="content">
    <div class="list" bind:this={list}>
      <div class="head">{i18n.t("main.listHead")}</div>
      <ul aria-label={i18n.t("main.notes")}>
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
          {i18n.t("main.name")}
          <input bind:this={nameField} bind:value={editor.name} maxlength={LIMITS.name} disabled={editor.locked} />
        </label>
        <label class="muted">
          {i18n.t("main.updated")}
          <input class="updated" value={updated} readonly tabindex="-1" />
        </label>
        <label class="muted">
          {i18n.t("main.length")}
          <input class="length" value="{utf16Len(editor.info)}/{LIMITS.info}" readonly tabindex="-1" />
        </label>
        <button class="history" onclick={() => (showHistory = true)} disabled={!b.history}>
          <img src={historyIcon} alt="" />{i18n.t("history.title")}
        </button>
      </div>
      <textarea bind:this={textField} bind:value={editor.info} maxlength={LIMITS.info} disabled={editor.locked} aria-label={i18n.t("main.text")}></textarea>
      <div class="actions">
        <button class="apply" type="submit" onclick={() => editor.apply()} disabled={!b.apply}>{i18n.t("main.apply")}</button>
        <button class="cancel" onclick={() => editor.cancel()} disabled={!b.cancel}>{i18n.t("main.cancel")}</button>
      </div>
    </section>
  </div>

  <footer class="bar">
    <button title={i18n.t("main.backupTitle")} onclick={onBackup} disabled={!sync.connected || !sync.dirty || sync.busy}>
      <img class="mono" src={saveIcon} alt="" />{i18n.t("main.backup")}
    </button>
    <button title={i18n.t("main.restoreTitle")} onclick={onRestore} disabled={!sync.connected || !sync.restoreAvailable || sync.busy}>
      <img class="mono" src={restoreIcon} alt="" />{i18n.t("main.restore")}
    </button>
    <span class="spacer"></span>
    <span class="credit">{i18n.t("main.license")}</span>
    <span class="credit">© Nikita Chisnikov, 2015-2026</span>
    <span class="spacer"></span>
    <button title={i18n.t("main.password")} aria-label={i18n.t("main.password")} onclick={() => (changingPassword = true)}>
      <img src={passwordIcon} alt="" />
    </button>
    <button title={i18n.t("main.settingsTitle")} onclick={onSettings}>{i18n.t("main.settings")}</button>
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

{#if privacyOpen}
  <PrivacyPolicy onClose={() => (privacyOpen = false)} />
{/if}

<style>
  .main {
    width: 100%;
    height: 100%;
    /* The screen is a grid item of <main>, whose row grows with the content:
       without this the row takes the height of the whole list, and the window
       scrolls instead of the list. */
    min-height: 0;
    display: grid;
    grid-template-rows: auto 1fr auto;
  }
  /* The top row shares the columns of the list and the editor below it:
     New and Remove over the list, the search over the editor; the buttons
     at the right end take a column of their own. */
  .find,
  .content {
    display: grid;
    grid-template-columns: 13.6rem 1fr;
    gap: 0.75rem;
  }
  .find {
    grid-template-columns: 13.6rem 1fr auto;
    align-items: stretch;
    padding: 0.75rem 0.75rem 0;
  }
  .tools {
    display: flex;
    gap: 0.5rem;
  }
  .notes {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.5rem;
  }
  .content {
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

  /* The editor group: fields, text, Apply | Cancel. */
  .editor {
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
    min-width: 0;
    min-height: 0;
    padding: 0.6rem;
    border: 1px solid var(--border);
  }
  /* The title never collapses: when the width is short, the date, the length and
     History wrap to a second line instead of squeezing it. */
  .fields {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.5rem 0.75rem;
  }
  .fields label {
    flex-direction: row;
    align-items: center;
    gap: 0.4rem;
    white-space: nowrap;
  }
  .fields .name {
    flex: 1 1 18rem;
    min-width: 16rem;
    color: inherit;
  }
  .name input {
    flex: 1;
    min-width: 10rem;
  }
  .updated,
  .length {
    color: var(--muted);
    text-align: center;
  }
  .updated {
    width: 10rem;
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
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 0.5rem;
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
    white-space: nowrap;
    color: var(--muted);
  }
  .credit + .credit {
    border-left: 1px solid var(--border);
  }
  .spacer {
    flex: 1;
  }
</style>
