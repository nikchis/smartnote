<script lang="ts">
  // CloudAccount is the account block of Settings for a cloud storage (spec
  // §8): the sign-in state of the draft's kind and client, and the way to
  // change it. The sign-in runs in Go and blocks until its end; phase tells
  // the dialog whether Test and OK must wait. account is undefined while the
  // form waits for Go to report it: the block then offers no sign-in or
  // sign-out. latest numbers the changes of account shared with the form, so
  // that the end of an older one does not overwrite a newer answer.
  import GoogleSignInButton from "./GoogleSignInButton.svelte";
  import { ConnectionService, type AccountDTO } from "../lib/api";
  import { dialogs } from "../lib/dialogs.svelte";
  import { messageOf, parseError } from "../lib/errors";
  import { i18n } from "../lib/i18n.svelte";
  import { fromForm, type CloudKind, type Latest, type SettingsForm, type SignInPhase } from "../lib/settings";

  let {
    kind,
    account = $bindable(),
    latest,
    form,
    phase = $bindable("idle"),
    disabled = false,
    invalid = $bindable(""),
  }: {
    kind: CloudKind;
    account: AccountDTO | undefined;
    latest: Latest;
    form: SettingsForm;
    phase?: SignInPhase;
    disabled?: boolean;
    invalid?: string;
  } = $props();

  /**
   * CLOUD is what differs between the clouds: the fields Go names in its
   * validation errors, and whether a sign-in can go on with a code the user
   * copies (Google Drive has none, spec §8).
   */
  const CLOUD: Record<CloudKind, { accountField: string; clientField: string; manual: boolean }> = {
    yandex_disk: { accountField: "yandex_account", clientField: "yandex_client_id", manual: true },
    google_drive: { accountField: "google_account", clientField: "google_client_id", manual: false },
  };
  const cloud = $derived(CLOUD[kind]);

  let code = $state("");
  /** attempt numbers the sign-ins, so that the end of an abandoned one is ignored. */
  let attempt = 0;
  /** current tells whether account is still the sign-in's to show: no later change began since it started. */
  let current: () => boolean = () => false;

  /** begin starts a sign-in attempt and claims the account it will show. */
  function begin(): number {
    current = latest.begin();
    return ++attempt;
  }

  /** report shows an error of Go; "canceled" is the user's own decision and silent. */
  async function report(e: unknown) {
    const err = parseError(e);
    invalid = err.code === "validation" ? (err.field ?? "") : "";
    if (err.code !== "canceled") await dialogs.error(messageOf(err));
  }

  function signedIn(login: string) {
    phase = "idle";
    if (current()) account = { account: login, signedIn: true };
  }

  async function signIn() {
    const mine = begin();
    invalid = "";
    phase = "browser";
    try {
      const login = await ConnectionService.SignIn(fromForm(form));
      if (mine !== attempt) return;
      signedIn(login);
    } catch (e) {
      if (mine !== attempt) return;
      if (cloud.manual && parseError(e).code === "loopback_unavailable") return enterManual(mine);
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
    const mine = begin();
    await ConnectionService.CancelSignIn().catch(() => {});
    if (mine === attempt) await enterManual(mine);
  }

  async function submit() {
    const mine = attempt;
    try {
      const login = await ConnectionService.SubmitCode(code);
      if (mine === attempt) signedIn(login);
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

  /** signOut marks the sign-out of this cloud in Go; Save makes it final, for this cloud only. */
  async function signOut() {
    if (signingOut) return;
    signingOut = true;
    const shown = latest.begin();
    try {
      await ConnectionService.SignOut(kind);
      if (shown()) account = { account: "", signedIn: false };
    } catch (e) {
      await report(e);
    } finally {
      signingOut = false;
    }
  }
</script>

<div class="account" data-testid="account" aria-invalid={invalid === cloud.accountField}>
  {#if phase === "browser"}
    <span class="muted">{i18n.t("account.waiting")}</span>
    <span class="spacer"></span>
    <button type="button" onclick={cancel}>{i18n.t("common.cancel")}</button>
    {#if cloud.manual}
      <button type="button" class="link" onclick={manual}>{i18n.t("account.manual")}</button>
    {/if}
  {:else if phase === "manual"}
    <label class="code">
      {i18n.t("account.code")}
      <!-- svelte-ignore a11y_autofocus -->
      <input aria-label={i18n.t("account.codeLabel")} bind:value={code} autocomplete="off" autofocus />
    </label>
    <button type="button" onclick={submit} disabled={code.trim() === ""}>{i18n.t("account.submit")}</button>
    <span class="spacer"></span>
    <button type="button" onclick={cancel}>{i18n.t("common.cancel")}</button>
  {:else if account === undefined}
    <!-- Go has not reported the sign-in of this draft yet. -->
    <span></span>
  {:else if account.signedIn}
    <span>{account.account ? i18n.t("account.signedInAs", { account: account.account }) : i18n.t("account.signedIn")}</span>
    <span class="spacer"></span>
    <!-- A new sign-in replaces the saved token on OK, e.g. after its access was revoked. -->
    <button type="button" onclick={signIn} {disabled}>{i18n.t("account.signInAgain")}</button>
    <button type="button" onclick={signOut} {disabled}>{i18n.t("account.signOut")}</button>
  {:else}
    <span class="muted">{i18n.t("account.notSignedIn")}</span>
    <span class="spacer"></span>
    {#if kind === "google_drive"}
      <GoogleSignInButton onclick={signIn} {disabled} />
    {:else}
      <button type="button" onclick={signIn} {disabled}>{i18n.t("account.signInYandex")}</button>
    {/if}
  {/if}
</div>
{#if invalid === cloud.clientField}
  <p class="hint">{i18n.t("account.ownClientHint")}</p>
{/if}

<style>
  /* As tall as the Google button in every state, so the form does not jump while Go answers. */
  .account {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    min-height: 40px;
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
