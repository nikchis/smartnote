<script lang="ts">
  // NewStorage creates the local storage on the first start.
  import { VaultService } from "../lib/api";
  import { dialogs } from "../lib/dialogs.svelte";
  import { parseError } from "../lib/errors";
  import { passwordPairOK } from "../lib/password";
  import { LIMITS } from "../lib/text";

  let { onCreated }: { onCreated: () => void } = $props();

  let password = $state("");
  let confirm = $state("");
  let busy = $state(false);
  const ready = $derived(passwordPairOK(password, confirm));

  async function create(e: SubmitEvent) {
    e.preventDefault();
    if (!ready || busy) return;
    busy = true;
    try {
      await VaultService.Create(password, confirm);
      password = confirm = "";
      onCreated();
    } catch (err) {
      await dialogs.error(parseError(err).message);
    } finally {
      busy = false;
    }
  }
</script>

<form class="card" onsubmit={create}>
  <h1>Smartnote</h1>
  <p>Create a password for the local storage ({LIMITS.passwordMin}–{LIMITS.passwordMax} characters).</p>
  <label>
    Password
    <input type="password" bind:value={password} autocomplete="new-password" />
  </label>
  <label>
    Confirm password
    <input type="password" bind:value={confirm} autocomplete="new-password" />
  </label>
  <button type="submit" disabled={!ready || busy}>Create storage</button>
</form>
