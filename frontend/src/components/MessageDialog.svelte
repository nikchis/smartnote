<script lang="ts">
  import errorIcon from "../assets/icons/msg-error.png";
  import infoIcon from "../assets/icons/msg-info.png";
  import questionIcon from "../assets/icons/msg-question.png";
  import { answer, currentDialog, dismiss } from "../lib/dialogs.svelte";
  import { modal } from "../lib/modal";

  const dialog = $derived(currentDialog());
  let primary: HTMLButtonElement | undefined = $state();

  $effect(() => {
    if (dialog) primary?.focus();
  });

  function onkeydown(e: KeyboardEvent) {
    if (!dialog) return;
    if (e.key === "Escape") {
      e.preventDefault();
      dismiss();
    }
  }

  const titles = { info: "Information", error: "Error", ask: "Question", choose: "Question" } as const;
  // The v1 message form showed one of three pictures left of the text.
  const icons = { info: infoIcon, error: errorIcon, ask: questionIcon, choose: questionIcon } as const;
</script>

<svelte:window {onkeydown} />

{#if dialog}
  <div class="backdrop" use:modal>
    <div class="dialog" role="alertdialog" aria-modal="true" aria-labelledby="dialog-title" aria-describedby="dialog-message">
      <img class="icon" src={icons[dialog.kind]} alt="" />
      <div class="body">
        <h2 id="dialog-title" class={dialog.kind}>{titles[dialog.kind]}</h2>
        <p id="dialog-message">{dialog.message}</p>
      </div>
      <div class="buttons">
        {#if dialog.kind === "ask" || dialog.kind === "choose"}
          <button bind:this={primary} onclick={() => answer("yes")}>{dialog.labels?.yes ?? "Yes"}</button>
          <button onclick={() => answer("no")}>{dialog.labels?.no ?? "No"}</button>
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
    display: grid;
    grid-template-columns: 64px 1fr;
    gap: 0 1rem;
    min-width: 24rem;
    max-width: 36rem;
    padding: 1rem 1.25rem;
    border-radius: 6px;
    background: var(--surface);
    box-shadow: 0 8px 24px rgb(0 0 0 / 0.3);
  }
  h2 {
    margin: 0 0 0.5rem;
    font-size: 1rem;
  }
  h2.error {
    color: var(--danger);
  }
  .icon {
    width: 64px;
    height: 64px;
  }
  p {
    margin: 0 0 1rem;
    white-space: pre-line;
    overflow-wrap: anywhere;
  }
  .buttons {
    grid-column: 1 / -1;
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
  }
</style>
