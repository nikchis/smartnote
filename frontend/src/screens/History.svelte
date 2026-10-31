<script lang="ts">
  // History lists the previous texts of a note by date; Use puts one into the
  // editor, which saves it only on Apply (v1 UDlgHistory).
  import type { VersionDTO } from "../lib/api";

  let { versions, onUse, onClose }: { versions: VersionDTO[]; onUse: (info: string) => void; onClose: () => void } = $props();

  let chosenId: string | undefined = $state();
  const chosen = $derived(versions.find((v) => v.id === chosenId) ?? versions[0]);

  function onkeydown(e: KeyboardEvent) {
    if (e.key === "Escape") onClose();
  }
</script>

<svelte:window {onkeydown} />

<div class="backdrop">
  <div class="card history" role="dialog" aria-modal="true" aria-label="History">
    <h2>History</h2>
    <div class="panes">
      <ul class="list" aria-label="Versions">
        {#each versions as v (v.id)}
          <li>
            <button class:active={chosen?.id === v.id} onclick={() => (chosenId = v.id)}>
              {new Date(v.created).toLocaleString()}
            </button>
          </li>
        {/each}
      </ul>
      <pre class="text">{chosen?.info ?? ""}</pre>
    </div>
    <div class="buttons">
      <button type="button" disabled={!chosen} onclick={() => chosen && onUse(chosen.info)}>Use</button>
      <button type="button" onclick={onClose}>Close</button>
    </div>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 5;
    display: grid;
    place-items: center;
    background: var(--backdrop);
  }
  .history {
    width: min(48rem, 90vw);
    height: min(32rem, 80vh);
  }
  .panes {
    flex: 1;
    min-height: 0;
    display: grid;
    grid-template-columns: 14rem 1fr;
    gap: 0.75rem;
  }
  .list {
    margin: 0;
    padding: 0;
    list-style: none;
    overflow: auto;
    border: 1px solid var(--border);
    border-radius: 4px;
  }
  .list button {
    width: 100%;
    text-align: left;
    border: none;
    border-radius: 0;
    background: none;
  }
  .list button.active {
    background: var(--accent);
    color: var(--surface);
  }
  .text {
    margin: 0;
    padding: 0.5rem;
    overflow: auto;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font: inherit;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg);
  }
</style>
