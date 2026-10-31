<script lang="ts">
  // ChangePassword sets a new password for the local storage (v1 UDlgPwd).
  import { VaultService } from "../lib/api";
  import { dialogs } from "../lib/dialogs.svelte";
  import { parseError } from "../lib/errors";
  import { passwordPairOK } from "../lib/password";
  import { LIMITS } from "../lib/text";

  let { onClose }: { onClose: () => void } = $props();

  let password = $state("");
  let confirm = $state("");
  let busy = $state(false);
  const ready = $derived(passwordPairOK(password, confirm));

  async function change(e: SubmitEvent) {
    e.preventDefault();
    if (!ready || busy) return;
    busy = true;
    try {
      await VaultService.ChangePassword(password, confirm);
      password = confirm = "";
      await dialogs.info("Password was successfully changed.");
      onClose();
    } catch (err) {
      await dialogs.error(parseError(err).message);
    } finally {
      busy = false;
    }
  }
</script>

<div class="backdrop">
  <form class="card" onsubmit={change}>
    <h2>Change password</h2>
    <label>
      New password
      <input type="password" bind:value={password} maxlength={LIMITS.passwordMax} autocomplete="new-password" />
    </label>
    <label>
      Confirm password
      <input type="password" bind:value={confirm} maxlength={LIMITS.passwordMax} autocomplete="new-password" />
    </label>
    <div class="buttons">
      <button type="submit" disabled={!ready || busy}>OK</button>
      <button type="button" onclick={onClose} disabled={busy}>Cancel</button>
    </div>
  </form>
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
</style>
