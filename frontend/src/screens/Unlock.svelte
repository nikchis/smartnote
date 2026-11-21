<script lang="ts">
  // Unlock opens the local storage; Enter submits.
  import { VaultService } from "../lib/api";
  import { dialogs } from "../lib/dialogs.svelte";
  import { errorMsg } from "../lib/errors";
  import { i18n } from "../lib/i18n.svelte";

  let { onUnlocked }: { onUnlocked: () => void } = $props();

  let password = $state("");
  let busy = $state(false);
  let input: HTMLInputElement | undefined = $state();

  $effect(() => input?.focus());

  async function unlock(e: SubmitEvent) {
    e.preventDefault();
    if (password === "" || busy) return;
    busy = true;
    try {
      await VaultService.Unlock(password);
      password = "";
      onUnlocked();
    } catch (err) {
      // wrong_password carries "Access to local storage denied.";
      // corrupt_storage and unsupported_format carry their own messages.
      await dialogs.error(errorMsg(err));
      password = "";
      input?.focus();
    } finally {
      busy = false;
    }
  }
</script>

<form class="card" onsubmit={unlock}>
  <label>
    {i18n.t("field.password")}
    <input type="password" bind:this={input} bind:value={password} autocomplete="current-password" />
  </label>
  <button type="submit" disabled={password === "" || busy}>{i18n.t("unlock.submit")}</button>
</form>
