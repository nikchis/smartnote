<script lang="ts">
  // Unlock opens the local storage; Enter submits.
  import { VaultService } from "../lib/api";
  import { dialogs } from "../lib/dialogs.svelte";
  import { parseError } from "../lib/errors";

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
      await dialogs.error(parseError(err).message);
      password = "";
      input?.focus();
    } finally {
      busy = false;
    }
  }
</script>

<form class="card" onsubmit={unlock}>
  <h1>Smartnote</h1>
  <label>
    Password
    <input type="password" bind:this={input} bind:value={password} autocomplete="current-password" />
  </label>
  <button type="submit" disabled={password === "" || busy}>Unlock</button>
</form>
