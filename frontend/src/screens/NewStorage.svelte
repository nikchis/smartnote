<script lang="ts">
  // NewStorage creates the local storage on the first start.
  import { VaultService } from "../lib/api";
  import { dialogs } from "../lib/dialogs.svelte";
  import { errorMsg } from "../lib/errors";
  import { i18n } from "../lib/i18n.svelte";
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
      await dialogs.error(errorMsg(err));
    } finally {
      busy = false;
    }
  }
</script>

<form class="card" onsubmit={create}>
  <p>{i18n.t("newStorage.prompt", { min: LIMITS.passwordMin, max: LIMITS.passwordMax })}</p>
  <label>
    {i18n.t("field.password")}
    <input type="password" bind:value={password} autocomplete="new-password" />
  </label>
  <label>
    {i18n.t("field.confirmPassword")}
    <input type="password" bind:value={confirm} autocomplete="new-password" />
  </label>
  <button type="submit" disabled={!ready || busy}>{i18n.t("newStorage.create")}</button>
</form>
