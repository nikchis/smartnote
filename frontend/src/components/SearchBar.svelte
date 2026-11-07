<script lang="ts">
  // SearchBar is the search field above the list and the editor, with the
  // hits under it (search spec §9.1): ↑/↓ choose, Enter or a click opens,
  // Escape closes and gives the focus back to where it was. Until the answer
  // to a changed query comes, the hits of the previous one stay dimmed and do
  // not open; Enter then opens a hit of the new answer.
  import type { SearchHitDTO } from "../lib/api";
  import { highlight, type SearchBox } from "../lib/search.svelte";

  /** MAX_QUERY is the most UTF-16 units of a query the worker reads; maxlength counts them too. */
  const MAX_QUERY = 256;

  let {
    box,
    nameOf,
    onOpen,
  }: { box: SearchBox; nameOf: (noteId: string) => string; onOpen: (hit: SearchHitDTO) => void } = $props();

  let field: HTMLInputElement | undefined = $state();
  let results: HTMLElement | undefined = $state();
  let open = $state(false);
  /** returnTo had the focus before the field; Escape gives it back. */
  let returnTo: HTMLElement | null = null;

  const info = $derived(box.statusInfo());
  const line = $derived(info.text);
  const shown = $derived(open && box.query.trim() !== "" && (box.hits.length > 0 || line !== ""));

  /**
   * focus puts the focus on the field with its text selected, as Ctrl+F does
   * in a browser, and shows the hits; the field may have the focus already,
   * as after a jump to a note that is gone, so no focus event opens them.
   */
  export function focus(): void {
    field?.focus();
    field?.select();
    open = true;
  }

  // The chosen row stays in view: the effect reads the selection and the hits,
  // so it runs again after the arrow keys or new hits have updated the rows.
  $effect(() => {
    const selected = box.selected;
    if (shown && box.hits.length > 0 && selected >= 0) {
      results?.querySelector(`#search-hit-${selected}`)?.scrollIntoView({ block: "nearest" });
    }
  });

  function onfocus(e: FocusEvent): void {
    const from = e.relatedTarget;
    returnTo = from instanceof HTMLElement && !results?.contains(from) ? from : null;
    open = true;
  }

  function onfocusout(e: FocusEvent): void {
    const to = e.relatedTarget;
    if (!(to instanceof Node && e.currentTarget instanceof HTMLElement && e.currentTarget.contains(to))) open = false;
  }

  function oninput(e: Event & { currentTarget: HTMLInputElement }): void {
    box.input(e.currentTarget.value);
    open = true;
  }

  /** choose opens hit i of the list; a hit of an earlier query (stale) is not opened. */
  function choose(i: number): void {
    const hit = box.hits[i];
    if (!hit || box.stale) return;
    box.selected = i;
    open = false;
    onOpen(hit);
  }

  /**
   * openFresh opens with Enter the chosen hit of the query in the field. While
   * the answer to it is pending, the hits on screen answer the previous query:
   * it searches at once and opens the chosen hit of the answer, unless the
   * query changed again meanwhile.
   */
  async function openFresh(): Promise<void> {
    if (!box.stale) {
      choose(box.selected);
      return;
    }
    const hit = await box.fresh();
    if (!hit) return;
    open = false;
    onOpen(hit);
  }

  function close(): void {
    open = false;
    const target = returnTo;
    returnTo = null;
    if (target?.isConnected && !target.closest("[inert]") && !target.matches(":disabled")) target.focus();
    else field?.blur();
  }

  function onkeydown(e: KeyboardEvent): void {
    switch (e.key) {
      case "ArrowDown":
      case "ArrowUp":
        e.preventDefault();
        if (open) box.move(e.key === "ArrowDown" ? 1 : -1);
        else open = true;
        break;
      case "Enter":
        e.preventDefault();
        if (open && box.query.trim() !== "") void openFresh();
        break;
      case "Escape":
        e.preventDefault();
        close();
        break;
    }
  }
</script>

<div class="search" role="search" {onfocusout}>
  <input
    bind:this={field}
    value={box.query}
    placeholder="Search records"
    aria-label="Search records"
    maxlength={MAX_QUERY}
    role="combobox"
    aria-autocomplete="list"
    aria-expanded={shown}
    aria-controls="search-results"
    aria-activedescendant={shown && box.selected >= 0 ? `search-hit-${box.selected}` : undefined}
    autocomplete="off"
    spellcheck="false"
    {onfocus}
    {oninput}
    {onkeydown}
  />
  {#if shown}
    <div class="dropdown">
      <ul id="search-results" role="listbox" aria-label="Search results" aria-busy={box.stale} bind:this={results}>
        {#each box.hits as hit, i (i)}
          <!-- The field keeps the focus: a click must not take it, the keys work in the field. -->
          <li
            id="search-hit-{i}"
            role="option"
            tabindex="-1"
            aria-selected={i === box.selected}
            class:active={i === box.selected}
            onmousedown={(e) => e.preventDefault()}
            onclick={() => choose(i)}
            onkeydown={onkeydown}
          >
            <div class="head">
              <span class="note">{nameOf(hit.noteId)}</span>
              <span class="where">{hit.line === 0 ? "name" : `line ${hit.line}`}</span>
            </div>
            <div class="snippet">
              {#each highlight(hit.snippet, hit.marks) as piece, j (j)}{#if piece.mark}<mark>{piece.text}</mark>{:else}{piece.text}{/if}{/each}
            </div>
          </li>
        {/each}
      </ul>
      {#if line}
        <div class="status" role="status">
          {line}
          {#if info.indexing}
            <progress max={info.indexing.total} value={info.indexing.done} aria-label="Indexing progress"></progress>
          {/if}
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .search {
    position: relative;
  }
  input {
    box-sizing: border-box;
    width: 100%;
  }
  input::placeholder {
    color: var(--muted);
  }

  /* The hits drop over the list and the editor, framed as the v1 panels. */
  .dropdown {
    position: absolute;
    top: calc(100% + 2px);
    left: 0;
    right: 0;
    z-index: 3;
    display: flex;
    flex-direction: column;
    max-height: 70vh;
    border: 1px solid var(--border);
    background: var(--row);
    box-shadow: 0 8px 24px rgb(0 0 0 / 0.3);
  }
  ul {
    margin: 0;
    padding: 0;
    overflow: auto;
    list-style: none;
  }
  li {
    padding: 0.3rem 0.5rem;
    border-bottom: 1px solid var(--border);
    cursor: pointer;
  }
  li:nth-child(odd) {
    background: var(--stripe);
  }
  li:hover {
    background: var(--button);
  }
  li.active {
    background: var(--accent);
  }
  /* The hits of the previous query while the answer to the new one is pending. */
  ul[aria-busy="true"] li {
    cursor: default;
    opacity: 0.55;
  }
  .head {
    display: flex;
    align-items: baseline;
    gap: 0.75rem;
  }
  .note {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .where {
    flex: none;
    color: var(--muted);
    font-size: 0.85rem;
  }
  li.active .where {
    color: var(--text);
  }
  .snippet {
    margin-top: 0.15rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: pre;
    font: var(--note-font);
    font-size: 0.85rem;
  }
  mark {
    border-radius: 2px;
    background: #8c7637;
    color: var(--text);
  }
  .status {
    display: flex;
    gap: 0.5rem;
    align-items: center;
    padding: 0.2rem 0.5rem;
    color: var(--muted);
    font-size: 0.85rem;
    background: var(--surface);
  }
  .status progress {
    flex: 1;
    height: 4px;
  }
</style>
