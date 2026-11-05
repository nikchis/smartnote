<script lang="ts">
  // Overlay covers the window during an operation with the server, as
  // "Connecting…" and "Synchronizing…" did in v1. While the storage reports
  // the progress of a transfer it shows it in place of the text.
  import { modal } from "../lib/modal";
  import type { ProgressView } from "../lib/progress";

  let { text, progress }: { text: string; progress?: ProgressView } = $props();
</script>

<div class="overlay" role="status" aria-live="polite" use:modal>
  <div class="box">
    {#if progress}
      <p>{progress.title}</p>
      <!-- The numbers change up to 10 times a second: the bar carries them for screen readers, the live region does not repeat them. -->
      <progress max={progress.total} value={progress.done} aria-label={progress.title} aria-valuetext={progress.detail}></progress>
      <p class="detail" aria-hidden="true">{progress.detail}</p>
      {#if progress.warning}
        <p class="warning">{progress.warning}</p>
      {/if}
    {:else}
      {text}
    {/if}
  </div>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    z-index: 10;
    display: grid;
    place-items: center;
    background: var(--backdrop);
    cursor: progress;
  }
  .box {
    max-width: 28rem;
    padding: 1rem 2rem;
    border-radius: 6px;
    background: var(--surface);
  }
  p {
    margin: 0.25rem 0;
  }
  progress {
    width: 100%;
    min-width: 18rem;
  }
  .detail {
    font-variant-numeric: tabular-nums;
  }
  .warning {
    margin-top: 0.75rem;
    color: var(--muted);
    font-size: 0.85rem;
  }
</style>
