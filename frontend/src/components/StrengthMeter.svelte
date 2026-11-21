<script lang="ts">
  // StrengthMeter hints how hard the sync phrase is to guess; it never blocks saving.
  import { i18n, type Key } from "../lib/i18n.svelte";
  import { strength, type Strength } from "../lib/strength";

  let { phrase }: { phrase: string } = $props();

  const score = $derived(strength(phrase));
  const labels: Record<Strength, Key> = { 0: "strength.0", 1: "strength.1", 2: "strength.2", 3: "strength.3", 4: "strength.4" };
</script>

<div class="meter" aria-live="polite">
  <div class="bars">
    {#each [0, 1, 2, 3] as i (i)}
      <span class:on={phrase !== "" && score > i} class="s{score}"></span>
    {/each}
  </div>
  <span class="label">{phrase === "" ? "" : i18n.t(labels[score])}</span>
</div>

<style>
  .meter {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    min-height: 1.2rem;
  }
  .bars {
    display: flex;
    gap: 3px;
  }
  .bars span {
    width: 2.5rem;
    height: 5px;
    border-radius: 2px;
    background: var(--border);
  }
  .bars span.on.s1 {
    background: var(--danger);
  }
  .bars span.on.s2 {
    background: #d97706;
  }
  .bars span.on.s3,
  .bars span.on.s4 {
    background: #16a34a;
  }
  .label {
    color: var(--muted);
    font-size: 0.9em;
  }
</style>
