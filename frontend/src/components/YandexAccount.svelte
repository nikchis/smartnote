<script lang="ts">
  // YandexAccount is the account block of Settings (spec §8): the sign-in state
  // and the way to change it. The sign-in runs in Go and blocks until its end;
  // phase tells the dialog whether Test and OK must wait.
  import { ConnectionService } from "../lib/api";
  import { dialogs } from "../lib/dialogs.svelte";
  import { messageOf, parseError } from "../lib/errors";
  import { i18n } from "../lib/i18n.svelte";
  import { fromForm, type SettingsForm, type SignInPhase } from "../lib/settings";

  let {
    form = $bindable(),
    phase = $bindable("idle"),
    disabled = false,
    invalid = $bindable(""),
  }: { form: SettingsForm; phase?: SignInPhase; disabled?: boolean; invalid?: string } = $props();

  let code = $state("");
  /** attempt numbers the sign-ins, so that the end of an abandoned one is ignored. */
  let attempt = 0;

  /** report shows an error of Go; "canceled" is the user's own decision and silent. */
  async function report(e: unknown) {
    const err = parseError(e);
    invalid = err.code === "validation" ? (err.field ?? "") : "";
    if (err.code !== "canceled") await dialogs.error(messageOf(err));
  }

  function signedIn(account: string) {
    form.yandexSignedIn = true;
    form.yandexAccount = account;
    phase = "idle";
  }

  async function signIn() {
    const mine = ++attempt;
    invalid = "";
    phase = "browser";
    try {
      const account = await ConnectionService.SignIn(fromForm(form));
      if (mine !== attempt) return;
      signedIn(account);
    } catch (e) {
      if (mine !== attempt) return;
      if (parseError(e).code === "loopback_unavailable") return enterManual(mine);
      phase = "idle";
      await report(e);
    }
  }

  async function enterManual(mine: number) {
    code = "";
    phase = "manual";
    try {
      await ConnectionService.SignInManual(fromForm(form));
    } catch (e) {
      if (mine !== attempt) return;
      phase = "idle";
      await report(e);
    }
  }

  async function manual() {
    const mine = ++attempt;
    await ConnectionService.CancelSignIn().catch(() => {});
    if (mine === attempt) await enterManual(mine);
  }

  async function submit() {
    const mine = attempt;
    try {
      const account = await ConnectionService.SubmitCode(code);
      if (mine === attempt) signedIn(account);
    } catch (e) {
      if (mine !== attempt) return;
      if (parseError(e).code === "canceled") phase = "idle";
      await report(e);
    }
  }

  /** cancel abandons a running sign-in; without one it does nothing. */
  export async function cancel() {
    if (phase === "idle") return;
    attempt++;
    phase = "idle";
    await ConnectionService.CancelSignIn().catch(() => {});
  }

  let signingOut = false;

  async function signOut() {
    if (signingOut) return;
    signingOut = true;
    try {
      await ConnectionService.SignOut();
      form.yandexSignedIn = false;
      form.yandexAccount = "";
    } catch (e) {
      await report(e);
    } finally {
      signingOut = false;
    }
  }
</script>

<div class="account" data-testid="account" aria-invalid={invalid === "yandex_account"}>
  {#if phase === "browser"}
    <span class="muted">{i18n.t("account.waiting")}</span>
    <span class="spacer"></span>
    <button type="button" onclick={cancel}>{i18n.t("common.cancel")}</button>
    <button type="button" class="link" onclick={manual}>{i18n.t("account.manual")}</button>
  {:else if phase === "manual"}
    <label class="code">
      {i18n.t("account.code")}
      <!-- svelte-ignore a11y_autofocus -->
      <input aria-label={i18n.t("account.codeLabel")} bind:value={code} autocomplete="off" autofocus />
    </label>
    <button type="button" onclick={submit} disabled={code.trim() === ""}>{i18n.t("account.submit")}</button>
    <span class="spacer"></span>
    <button type="button" onclick={cancel}>{i18n.t("common.cancel")}</button>
  {:else if form.yandexSignedIn}
    <span>{form.yandexAccount ? i18n.t("account.signedInAs", { account: form.yandexAccount }) : i18n.t("account.signedIn")}</span>
    <span class="spacer"></span>
    <!-- A new sign-in replaces the saved token on OK, e.g. after its access was revoked. -->
    <button type="button" onclick={signIn} {disabled}>{i18n.t("account.signInAgain")}</button>
    <button type="button" onclick={signOut} {disabled}>{i18n.t("account.signOut")}</button>
  {:else}
    <span class="muted">{i18n.t("account.notSignedIn")}</span>
    <span class="spacer"></span>
    <button type="button" onclick={signIn} {disabled}>{i18n.t("account.signInYandex")}</button>
  {/if}
</div>
{#if invalid === "yandex_client_id"}
  <p class="hint">{i18n.t("account.ownClientHint")}</p>
{/if}

<style>
  .account {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.25rem;
    border: 1px solid transparent;
    border-radius: 4px;
  }
  .account[aria-invalid="true"] {
    border-color: var(--danger);
  }
  .spacer {
    flex: 1;
  }
  .muted {
    color: var(--muted);
  }
  /* The global label is a column; the code field stays beside its caption. */
  .code {
    display: flex;
    flex-direction: row;
    align-items: center;
    gap: 0.5rem;
  }
  .link {
    border: 0;
    background: none;
    color: var(--text);
    text-decoration: underline;
    cursor: pointer;
  }
  .link:focus-visible {
    outline: 1px dotted var(--text);
    outline-offset: 2px;
  }
  .hint {
    margin: 0.25rem 0 0;
    color: var(--danger);
  }
</style>
