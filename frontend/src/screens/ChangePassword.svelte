<script lang="ts">
  // ChangePassword sets a new password for the local storage (v1 UDlgPwd).
  import { VaultService } from "../lib/api";
  import { dialogs } from "../lib/dialogs.svelte";
  import { errorMsg } from "../lib/errors";
  import { i18n, msg } from "../lib/i18n.svelte";
  import { modal } from "../lib/modal";
  import { passwordPairOK } from "../lib/password";

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
      await dialogs.info(msg("changePassword.done"));
      onClose();
    } catch (err) {
      await dialogs.error(errorMsg(err));
    } finally {
      busy = false;
    }
  }
</script>

<div class="backdrop" use:modal>
  <form class="card" onsubmit={change}>
    <h2>{i18n.t("changePassword.title")}</h2>
    <label>
      {i18n.t("changePassword.new")}
      <input type="password" bind:value={password} autocomplete="new-password" />
    </label>
    <label>
      {i18n.t("field.confirmPassword")}
      <input type="password" bind:value={confirm} autocomplete="new-password" />
    </label>
    <div class="buttons">
      <button type="submit" disabled={!ready || busy}>{i18n.t("common.ok")}</button>
      <button type="button" onclick={onClose} disabled={busy}>{i18n.t("common.cancel")}</button>
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
