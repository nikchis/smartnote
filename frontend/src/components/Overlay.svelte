<script lang="ts">
  // Overlay covers the window during an operation with the server, as
  // "Connecting…" and "Synchronizing…" did in v1.
  let { text }: { text: string } = $props();
</script>

<div class="overlay" role="status" aria-live="polite">
  <div class="box">{text}</div>
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
    padding: 1rem 2rem;
    border-radius: 6px;
    background: var(--surface);
  }
</style>
