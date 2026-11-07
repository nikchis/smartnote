<script lang="ts">
  import { answer, currentDialog, dismiss, type DialogKind } from "../lib/dialogs.svelte";
  import { modal } from "../lib/modal";

  const dialog = $derived(currentDialog());
  /** primary is OK or Yes; a question may give the focus to No instead (Labels.primary). */
  let primary: HTMLButtonElement | undefined = $state();
  let noButton: HTMLButtonElement | undefined = $state();

  $effect(() => {
    if (dialog) (dialog.labels?.primary === "no" ? noButton : primary)?.focus();
  });

  function onkeydown(e: KeyboardEvent) {
    if (!dialog) return;
    if (e.key === "Escape") {
      e.preventDefault();
      dismiss();
    }
  }

  // The look of the sign-in pages (internal/adapter/yadisk/pages.go): a
  // centered card with an outlined mark. info reports a done action, so it
  // shows a check mark and no title.
  type Mark = "success" | "warning" | "error" | "question";
  const marks: Record<DialogKind, Mark> = { info: "success", warning: "warning", error: "error", ask: "question", choose: "question" };
  const titles: Record<DialogKind, string | null> = { info: null, warning: "Warning", error: "Error", ask: "Question", choose: "Question" };
</script>

<svelte:window {onkeydown} />

{#if dialog}
  {@const mark = marks[dialog.kind]}
  {@const title = titles[dialog.kind]}
  <div class="backdrop" use:modal>
    <div
      class="dialog"
      role="alertdialog"
      aria-modal="true"
      aria-labelledby={title ? "dialog-title" : "dialog-message"}
      aria-describedby={title ? "dialog-message" : undefined}
    >
      <svg class="mark {mark}" data-mark={mark} viewBox="0 0 48 48" aria-hidden="true">
        <circle cx="24" cy="24" r="22" />
        {#if mark === "success"}
          <path d="M14 25 l7 7 l13 -14" />
        {:else if mark === "question"}
          <path d="M18.5 18.5 a5.5 5.5 0 1 1 8 4.9 c-1.6 .9 -2.5 2 -2.5 3.6 v1" />
          <circle class="dot" cx="24" cy="34" r="2.3" />
        {:else}
          <path d="M24 13 v14" />
          <circle class="dot" cx="24" cy="34" r="2.3" />
        {/if}
      </svg>
      {#if title}
        <h2 id="dialog-title" class={dialog.kind}>{title}</h2>
      {/if}
      <p id="dialog-message">{dialog.message}</p>
      <div class="buttons">
        {#if dialog.kind === "ask" || dialog.kind === "choose"}
          <button bind:this={primary} onclick={() => answer("yes")}>{dialog.labels?.yes ?? "Yes"}</button>
          <button bind:this={noButton} onclick={() => answer("no")}>{dialog.labels?.no ?? "No"}</button>
          {#if dialog.kind === "choose"}
            <button onclick={() => answer("cancel")}>Cancel</button>
          {/if}
        {:else}
          <button bind:this={primary} onclick={() => answer("yes")}>OK</button>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 20;
    display: grid;
    place-items: center;
    background: var(--backdrop);
  }
  .dialog {
    box-sizing: border-box;
    width: min(26rem, calc(100vw - 2rem));
    max-height: calc(100vh - 2rem);
    overflow-y: auto;
    padding: 1.75rem 2rem 1.25rem;
    border: 1px solid var(--border);
    border-radius: 10px;
    background: var(--surface);
    box-shadow: 0 12px 32px rgb(0 0 0 / 0.28);
    text-align: center;
  }
  .mark {
    display: block;
    width: 44px;
    height: 44px;
    margin: 0 auto 0.75rem;
    fill: none;
    stroke: var(--mark);
    stroke-width: 3.5;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .mark > circle:first-child {
    stroke-width: 3;
  }
  .mark .dot {
    fill: var(--mark);
    stroke: none;
  }
  .mark.success {
    --mark: #7fb37a;
  }
  .mark.warning {
    --mark: #df9e25;
  }
  .mark.error {
    --mark: var(--danger);
  }
  .mark.question {
    --mark: var(--accent);
  }
  h2 {
    margin: 0 0 0.5rem;
    font-size: 1.2rem;
    font-weight: 700;
  }
  h2.error {
    color: var(--danger);
  }
  p {
    margin: 0;
    white-space: pre-line;
    overflow-wrap: anywhere;
  }
  .buttons {
    display: flex;
    justify-content: center;
    gap: 0.5rem;
    margin-top: 1.25rem;
    padding-top: 1rem;
    border-top: 1px solid var(--border);
  }
</style>
