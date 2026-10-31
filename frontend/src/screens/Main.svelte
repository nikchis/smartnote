<script lang="ts">
  // Main lists the notes and edits the selected one (v1 UMain). The bottom
  // bar starts the actions of task 14 through the props.
  import { NotesService, type StateDTO } from "../lib/api";
  import { dialogs } from "../lib/dialogs.svelte";
  import { Editor } from "../lib/editor.svelte";
  import { LIMITS, utf16Len } from "../lib/text";
  import ChangePassword from "./ChangePassword.svelte";
  import History from "./History.svelte";

  let {
    sync,
    onSettings,
    onBackup,
    onRestore,
  }: { sync: StateDTO; onSettings: () => void; onBackup: () => void; onRestore: () => void } = $props();

  const editor = new Editor(NotesService, dialogs);
  const b = $derived(editor.buttons);
  let showHistory = $state(false);
  let changingPassword = $state(false);

  /** reload reads the notes again, after a Restore; an unsaved edit is lost. */
  export function reload(): Promise<void> {
    return editor.load();
  }

  $effect(() => {
    void editor.load();
  });

  const updated = $derived(editor.mode === "edit" && editor.selected ? new Date(editor.selected.updated).toLocaleString() : "");
</script>

<div class="main">
  <aside class="list" aria-label="Notes">
    <ul>
      {#each editor.notes as note (note.id)}
        <li>
          <button
            class:active={editor.selected?.id === note.id}
            disabled={!b.list}
            onclick={() => editor.select(note.id)}
          >
            <span class="name">{note.name}</span>
            <span class="date">{new Date(note.updated).toLocaleString()}</span>
          </button>
        </li>
      {/each}
    </ul>
  </aside>

  <section class="editor">
    <label>
      Name
      <input bind:value={editor.name} maxlength={LIMITS.name} disabled={editor.locked} />
    </label>
    <label class="grow">
      Text
      <textarea bind:value={editor.info} maxlength={LIMITS.info} disabled={editor.locked}></textarea>
    </label>
    <div class="meta">
      <span>{utf16Len(editor.info)}/{LIMITS.info}</span>
      <span>{updated}</span>
    </div>
    <div class="buttons left">
      <button onclick={() => editor.startNew()} disabled={!b.new}>New</button>
      <button type="submit" onclick={() => editor.apply()} disabled={!b.apply}>Apply</button>
      <button onclick={() => editor.cancel()} disabled={!b.cancel}>Cancel</button>
      <button onclick={() => editor.remove()} disabled={!b.delete}>Delete</button>
      <button onclick={() => (showHistory = true)} disabled={!b.history}>History</button>
    </div>
  </section>

  <footer class="bar">
    <button onclick={() => (changingPassword = true)}>Change password</button>
    <button onclick={onSettings}>Settings</button>
    <span class="spacer"></span>
    <button onclick={onBackup} disabled={!sync.connected || !sync.dirty || sync.busy}>Backup</button>
    <button onclick={onRestore} disabled={!sync.connected || !sync.restoreAvailable || sync.busy}>Restore</button>
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
    grid-template-columns: 16rem 1fr;
    grid-template-rows: 1fr auto;
  }
  .list {
    overflow: auto;
    border-right: 1px solid var(--border);
    background: var(--surface);
  }
  .list ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .list button {
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.1rem;
    border: none;
    border-radius: 0;
    border-bottom: 1px solid var(--border);
    background: none;
    text-align: left;
  }
  .list button.active {
    background: var(--accent);
    color: var(--surface);
  }
  .list .name {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .list .date {
    font-size: 0.85em;
    opacity: 0.75;
  }
  .editor {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    min-height: 0;
    padding: 0.75rem;
  }
  .grow {
    flex: 1;
    min-height: 0;
  }
  textarea {
    flex: 1;
    resize: none;
  }
  .meta {
    display: flex;
    justify-content: space-between;
    color: var(--muted);
  }
  .left {
    justify-content: flex-start;
  }
  .bar {
    grid-column: 1 / -1;
    display: flex;
    gap: 0.5rem;
    padding: 0.5rem 0.75rem;
    border-top: 1px solid var(--border);
    background: var(--surface);
  }
  .spacer {
    flex: 1;
  }
</style>
