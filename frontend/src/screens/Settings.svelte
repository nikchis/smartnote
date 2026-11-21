<script lang="ts" module>
  /** CLIENT_FIELDS are the own client fields of Advanced as Go names them in its validation errors. */
  const CLIENT_FIELDS = ["yandex_client_id", "google_client_id", "google_client_secret"];
</script>

<script lang="ts">
  // Settings edits the connection, the client SSH key and the trusted server
  // key (v1 UDlgConnection). The form holds the server key's fingerprint only;
  // a key trusted during Test, like a reset, is saved only with OK (spec §6.1).
  import { onDestroy, onMount } from "svelte";
  import exportIcon from "../assets/icons/export.png";
  import eyeIcon from "../assets/icons/eye.png";
  import gearIcon from "../assets/icons/gear.png";
  import importIcon from "../assets/icons/import.png";
  import StrengthMeter from "../components/StrengthMeter.svelte";
  import CloudAccount from "../components/CloudAccount.svelte";
  import { ConnectionService, KeyService, type AccountDTO } from "../lib/api";
  import { dialogs } from "../lib/dialogs.svelte";
  import { errorMsg, messageOf, parseError, type UIError } from "../lib/errors";
  import { GOOGLE_DOCS, openExternal, YANDEX_DOCS } from "../lib/external";
  import { i18n, msg, type Key } from "../lib/i18n.svelte";
  import { emptySettings, fromForm, isCloud, Latest, toForm, TYPING_PAUSE_MS, type SettingsForm, type SignInPhase } from "../lib/settings";
  import { modal } from "../lib/modal";
  import { settingsButtons } from "../lib/rules";
  import { LIMITS, secretLen } from "../lib/text";
  import { trustQuestion } from "../flows/sync";

  let { onSaved, onClose }: { onSaved: () => void; onClose: () => void } = $props();

  let form: SettingsForm = $state(toForm(emptySettings));
  /** loaded and hasKey stay unset until loaded, or when loading failed. */
  let loaded = $state(false);
  let hasKey: boolean | undefined = $state();
  let phase: SignInPhase = $state("idle");
  const allowed = $derived(settingsButtons(loaded, hasKey, form.kind, phase !== "idle"));
  /** busy names the running action, shown beside OK; "" when none runs. */
  let busy: Key | "" = $state("");
  let invalid = $state("");
  /** phraseShown reveals the sync phrase; every opening of the dialog starts hidden. */
  let phraseShown = $state(false);
  let passphrase: { value: string; retry: boolean } | undefined = $state();
  /** account is the sign-in of the draft's cloud as Go reports it; undefined until it answers. */
  let account: AccountDTO | undefined = $state();
  /** accountTurn numbers the changes of account: the reads here, the sign-ins and Sign outs of the block. */
  const accountTurn = new Latest();
  /**
   * advancedOpen is the user's choice to show Advanced, one for both clouds:
   * it outlives the errors and the changes of the kind, which recreate the
   * block, until the dialog closes. A validation error of an own client field
   * opens it, since the field to fix is there.
   */
  let advancedOpen = $state(false);
  $effect(() => {
    if (CLIENT_FIELDS.includes(invalid)) advancedOpen = true;
  });
  /** typing is the read of the account due when typing in an own client field pauses. */
  let typing: ReturnType<typeof setTimeout> | undefined;
  // A read that starts after a sign-in would outdate the sign-in's answer.
  $effect(() => {
    if (phase !== "idle") cancelTyping();
  });

  onMount(async () => {
    try {
      form = toForm(await ConnectionService.Load());
      loaded = true;
      void readAccount();
      hasKey = await KeyService.HasKey();
    } catch (e) {
      await dialogs.error(errorMsg(e));
    }
  });

  /**
   * readAccount asks Go for the sign-in of the kind and the client of the
   * draft (spec §8), when the dialog opens, the kind changes, an own client
   * field is left changed or typing in it pauses. Go counts the pending
   * sign-ins and the Sign outs of this dialog; the saved settings say nothing
   * about them. An answer for an older draft is dropped. Another kind shows
   * its block without buttons until the answer comes; keep leaves the block
   * of the same kind as it is, since leaving the field fires change on the
   * mousedown of the button the click is for. A sign-in or Sign out started meanwhile
   * outdates the answer, and SignIn sends the draft as it is then.
   */
  async function readAccount(keep = false) {
    const current = accountTurn.begin();
    if (!keep) account = undefined;
    if (!isCloud(form.kind)) return;
    try {
      const answer = await ConnectionService.Account(fromForm(form));
      if (current()) account = answer;
    } catch (e) {
      if (!current()) return;
      account = { account: "", signedIn: false };
      await dialogs.error(errorMsg(e));
    }
  }

  /** clientTyped reads the account once typing in an own client field pauses; change fires only when the field is left. */
  function clientTyped() {
    clearTimeout(typing);
    typing = setTimeout(() => {
      typing = undefined;
      void readAccount(true);
    }, TYPING_PAUSE_MS);
  }

  /** clientLeft reads the account at once, so that no read starts after a sign-in the click that left the field begins. */
  function clientLeft() {
    cancelTyping();
    void readAccount(true);
  }

  /** clientBlurred reads the account still due: a field typed back to its value at focus fires no change. */
  function clientBlurred() {
    if (typing !== undefined) clientLeft();
  }

  function cancelTyping() {
    clearTimeout(typing);
    typing = undefined;
  }

  /** kindChanged reads the account of the new kind; a read due for the old one is dropped. */
  function kindChanged() {
    cancelTyping();
    void readAccount();
  }

  onDestroy(() => clearTimeout(typing));

  async function fail(e: unknown) {
    const err: UIError = parseError(e);
    invalid = err.code === "validation" ? (err.field ?? "") : "";
    if (err.code !== "canceled" && err.code !== "busy") await dialogs.error(messageOf(err));
  }

  async function run(label: Key, f: () => Promise<void>) {
    if (busy) return;
    busy = label;
    try {
      await f();
    } catch (e) {
      await fail(e);
    } finally {
      busy = "";
    }
  }

  function test() {
    return run("overlay.connecting", async () => {
      if (!allowed.test) return;
      invalid = "";
      let empty: boolean;
      try {
        empty = await ConnectionService.Test(fromForm(form));
      } catch (e) {
        const err = parseError(e);
        if (err.code !== "host_key_unknown" || !err.fingerprint || !(await dialogs.ask(trustQuestion(err)))) throw e;
        form = toForm(await ConnectionService.TrustOfferedKey(err.fingerprint, fromForm(form)));
        empty = await ConnectionService.Test(fromForm(form));
      }
      await dialogs.info(msg(empty ? "settings.connectedEmpty" : "settings.connected"));
    });
  }

  function save() {
    return run("settings.saving", async () => {
      if (!allowed.save) return;
      invalid = "";
      await ConnectionService.Save(fromForm(form));
      onSaved();
    });
  }

  function resetTrust() {
    form.trustedFingerprint = "";
  }

  function generate() {
    return run("settings.generating", async () => {
      if (!allowed.generate) return;
      if (hasKey && !(await dialogs.ask(msg("settings.generateConfirm")))) return;
      await KeyService.Generate();
      hasKey = true;
      await dialogs.info(msg("settings.generated"));
    });
  }

  function exportPublic() {
    return run("settings.exporting", async () => {
      const saved = await KeyService.ExportPublicToFile();
      await dialogs.info(msg(saved ? "settings.exportedToFile" : "settings.exported"));
    });
  }

  function importKey(pass = "") {
    return run("settings.importing", async () => {
      if (!allowed.import) return;
      try {
        if (!(await KeyService.ImportFromFile(pass))) return;
      } catch (e) {
        if (parseError(e).code !== "key_passphrase_required") throw e;
        passphrase = { value: "", retry: pass !== "" };
        return;
      }
      hasKey = true;
      await dialogs.info(msg("settings.imported"));
    });
  }

  function submitPassphrase(e: SubmitEvent) {
    e.preventDefault();
    const value = passphrase?.value ?? "";
    passphrase = undefined;
    void importKey(value);
  }

  function cancelPassphrase() {
    passphrase = undefined;
    void KeyService.CancelImport();
  }

  /** close abandons a running sign-in before the dialog goes away; the account block may already be gone. */
  async function close() {
    if (phase !== "idle") await ConnectionService.CancelSignIn().catch(() => {});
    onClose();
  }

  const phraseShort = $derived(form.syncPhrase !== "" && secretLen(form.syncPhrase) < LIMITS.phraseMin);
</script>

<div class="backdrop" use:modal>
  <form class="card settings" onsubmit={(e) => (e.preventDefault(), save())}>
    <h2>{i18n.t("settings.title")}</h2>

    <section class="group storage" role="group" aria-labelledby="settings-storage">
      <span class="legend" id="settings-storage">{i18n.t("settings.storage")}</span>
      <div class="fields">
        <div class="wide row">
          <label class="choice"><input type="radio" name="kind" value="none" bind:group={form.kind} onchange={kindChanged} disabled={phase !== "idle"} />{i18n.t("settings.none")}</label>
          <label class="choice"><input type="radio" name="kind" value="yandex_disk" bind:group={form.kind} onchange={kindChanged} disabled={phase !== "idle"} />Yandex Disk</label>
          <label class="choice"><input type="radio" name="kind" value="google_drive" bind:group={form.kind} onchange={kindChanged} disabled={phase !== "idle"} />Google Drive</label>
          <label class="choice"><input type="radio" name="kind" value="ssh_postgres" bind:group={form.kind} onchange={kindChanged} disabled={phase !== "idle"} />SSH + PostgreSQL</label>
        </div>
      </div>
    </section>

    {#if isCloud(form.kind)}
      {@const google = form.kind === "google_drive"}
      <section class="group cloud" role="group" aria-labelledby={google ? "settings-google" : "settings-yandex"}>
        <span class="legend" id={google ? "settings-google" : "settings-yandex"}>{i18n.t(google ? "settings.googleAccount" : "settings.yandexAccount")}</span>
        <div class="fields">
          <div class="wide">
            <CloudAccount kind={form.kind} bind:account latest={accountTurn} {form} bind:phase bind:invalid disabled={!!busy} />
          </div>
          {#if google}
            <details class="wide" bind:open={advancedOpen}>
              <summary>{i18n.t("settings.advanced")}</summary>
              <label class="wide">
                {i18n.t("settings.ownClientId")}
                <input aria-label={i18n.t("settings.ownClientIdLabel")} bind:value={form.googleClientId} oninput={clientTyped} onchange={clientLeft} onblur={clientBlurred} autocomplete="off" aria-invalid={invalid === "google_client_id"} />
              </label>
              <label class="wide">
                {i18n.t("settings.ownClientSecret")}
                <input type="password" aria-label={i18n.t("settings.ownClientSecretLabel")} bind:value={form.googleClientSecret} oninput={clientTyped} onchange={clientLeft} onblur={clientBlurred} autocomplete="off" aria-invalid={invalid === "google_client_secret"} />
              </label>
              <a href={GOOGLE_DOCS} onclick={(e) => (e.preventDefault(), void openExternal(GOOGLE_DOCS))}>{i18n.t("settings.howToRegister")}</a>
              <p class="muted">{i18n.t("settings.otherGoogleClient")}</p>
            </details>
          {:else}
            <details class="wide" bind:open={advancedOpen}>
              <summary>{i18n.t("settings.advanced")}</summary>
              <label class="wide">
                {i18n.t("settings.ownClientId")}
                <input aria-label={i18n.t("settings.ownClientIdLabel")} bind:value={form.yandexClientId} oninput={clientTyped} onchange={clientLeft} onblur={clientBlurred} autocomplete="off" aria-invalid={invalid === "yandex_client_id"} />
              </label>
              <a href={YANDEX_DOCS} onclick={(e) => (e.preventDefault(), void openExternal(YANDEX_DOCS))}>{i18n.t("settings.howToRegister")}</a>
              <p class="muted">{i18n.t("settings.otherClient")}</p>
            </details>
          {/if}
        </div>
      </section>
    {/if}

    <!-- SSH server and Database side by side, as in v1 UDlgConnection. -->
    {#if form.kind === "ssh_postgres"}
    <div class="pair">
      <section class="group" role="group" aria-labelledby="settings-ssh">
        <span class="legend" id="settings-ssh">{i18n.t("settings.sshServer")}</span>
        <div class="fields">
          <label class="wide">{i18n.t("settings.host")} <input bind:value={form.sshHost} aria-invalid={invalid === "ssh_host"} /></label>
          <label>{i18n.t("settings.port")} <input bind:value={form.sshPort} inputmode="numeric" placeholder="22" aria-invalid={invalid === "ssh_port"} /></label>
          <label>{i18n.t("settings.user")} <input bind:value={form.sshUser} aria-invalid={invalid === "ssh_user"} /></label>
          <label>{i18n.t("field.password")} <input type="password" bind:value={form.sshPassword} autocomplete="off" /></label>
        </div>
      </section>

      <section class="group" role="group" aria-labelledby="settings-db">
        <span class="legend" id="settings-db">{i18n.t("settings.database")}</span>
        <div class="fields">
          <label>{i18n.t("settings.host")} <input bind:value={form.dbHost} placeholder="127.0.0.1" aria-invalid={invalid === "db_host"} /></label>
          <label>{i18n.t("settings.port")} <input bind:value={form.dbPort} inputmode="numeric" placeholder="5432" aria-invalid={invalid === "db_port"} /></label>
          <label>{i18n.t("settings.dbName")} <input bind:value={form.dbName} aria-invalid={invalid === "db_name"} /></label>
          <label>{i18n.t("settings.user")} <input bind:value={form.dbUser} aria-invalid={invalid === "db_user"} /></label>
          <label>{i18n.t("field.password")} <input type="password" bind:value={form.dbPassword} autocomplete="off" /></label>
        </div>
      </section>
    </div>
    {/if}

    {#if form.kind !== "none"}
    <section class="group" role="group" aria-labelledby="settings-phrase">
      <!-- The v1 group of the salt had the gear too. -->
      <span class="legend" id="settings-phrase"><img src={gearIcon} alt="" />{i18n.t("settings.syncPhrase")}</span>
      <div class="fields">
        <label class="wide">
          {i18n.t("settings.phrase", { n: LIMITS.phraseMin })}
          <span class="reveal-row">
            <input type={phraseShown ? "text" : "password"} bind:value={form.syncPhrase} autocomplete="off" aria-invalid={invalid === "sync_phrase" || phraseShort} />
            <button class="reveal" type="button" aria-pressed={phraseShown} aria-label={i18n.t(phraseShown ? "settings.hidePhrase" : "settings.showPhrase")} title={i18n.t(phraseShown ? "settings.hidePhrase" : "settings.showPhrase")} onclick={() => (phraseShown = !phraseShown)}>
              <img class="mono" src={eyeIcon} alt="" />
            </button>
          </span>
        </label>
        <div class="wide"><StrengthMeter phrase={form.syncPhrase} /></div>
      </div>
    </section>
    {/if}

    {#if form.kind === "ssh_postgres"}
    <section class="group" role="group" aria-labelledby="settings-keys">
      <span class="legend" id="settings-keys">{i18n.t("settings.keys")}</span>
      <div class="fields">
        <div class="wide row">
          <span class="muted">{hasKey === undefined ? "" : i18n.t(hasKey ? "settings.keySet" : "settings.noKey")}</span>
          <span class="spacer"></span>
          <button type="button" onclick={generate} disabled={!!busy || !allowed.generate}><img src={gearIcon} alt="" />{i18n.t("settings.generate")}</button>
          <button type="button" onclick={exportPublic} disabled={!!busy || !allowed.export}><img src={exportIcon} alt="" />{i18n.t("settings.exportPublic")}</button>
          <button type="button" onclick={() => importKey()} disabled={!!busy || !allowed.import}><img src={importIcon} alt="" />{i18n.t("settings.import")}</button>
        </div>
        <div class="wide row">
          <span class="muted" aria-invalid={invalid === "trusted_host_key"}>
            {form.trustedFingerprint ? i18n.t("settings.serverKey", { fingerprint: form.trustedFingerprint }) : i18n.t("settings.notTrusted")}
          </span>
          <span class="spacer"></span>
          <button type="button" onclick={resetTrust} disabled={!!busy || !form.trustedFingerprint}>{i18n.t("settings.reset")}</button>
        </div>
      </div>
    </section>
    {/if}

    <!-- OK on the left, Test connection in the middle, Cancel on the right, as in v1. -->
    <div class="actions">
      <div class="row">
        <button type="submit" disabled={!!busy || !allowed.save}>{i18n.t("common.ok")}</button>
        <span class="muted">{busy && i18n.t(busy)}</span>
      </div>
      <button type="button" onclick={test} disabled={!!busy || !allowed.test}>{i18n.t("settings.test")}</button>
      <button class="cancel" type="button" onclick={close} disabled={!!busy}>{i18n.t("common.cancel")}</button>
    </div>
  </form>
</div>

{#if passphrase}
  <div class="backdrop top" use:modal>
    <form class="card" onsubmit={submitPassphrase}>
      <h2>{i18n.t("settings.passphraseTitle")}</h2>
      <label>
        {i18n.t(passphrase.retry ? "settings.passphraseRetry" : "settings.passphrase")}
        <!-- svelte-ignore a11y_autofocus -->
        <input type="password" bind:value={passphrase.value} autocomplete="off" autofocus />
      </label>
      <div class="buttons">
        <button type="submit" disabled={passphrase.value === ""}>{i18n.t("common.ok")}</button>
        <button type="button" onclick={cancelPassphrase}>{i18n.t("common.cancel")}</button>
      </div>
    </form>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 5;
    display: grid;
    place-items: center;
    background: var(--backdrop);
  }
  .backdrop.top {
    z-index: 6;
  }
  .settings {
    width: min(60rem, 94vw);
    max-height: 94vh;
    overflow: auto;
  }
  .pair {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    align-items: start;
    gap: 0.75rem;
  }
  /* A section, not a fieldset: WebKitGTK lays out fieldset+legend ~19px too tall on first layout. */
  .group {
    position: relative;
    margin: 0.55em 0 0;
    padding: 0.95em 0.75em 0.625em;
    border: 1px solid var(--border);
    border-radius: 4px;
  }
  .legend {
    position: absolute;
    top: 0;
    left: 0.6em;
    transform: translateY(-50%);
    padding: 0 0.3em;
    background: var(--surface);
    line-height: 1.2;
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
  }
  .fields {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 0.5rem 0.75rem;
    align-content: start;
  }
  .wide {
    grid-column: 1 / -1;
  }
  .reveal-row {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: 0.5rem;
    align-items: center;
  }
  /* The eye glyph is black; the dark skin shows it light. */
  img.mono {
    filter: invert(1);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .spacer {
    flex: 1;
  }
  .muted {
    color: var(--muted);
  }
  .choice {
    display: inline-flex;
    flex-direction: row;
    align-items: center;
    gap: 0.4rem;
  }
  details > :global(*) {
    margin-top: 0.4rem;
  }
  input[aria-invalid="true"] {
    border-color: var(--danger);
  }
  .actions {
    display: grid;
    grid-template-columns: 1fr auto 1fr;
    align-items: center;
    gap: 0.5rem;
  }
  .cancel {
    justify-self: end;
  }
  /* Labels are bright here, as the captions of the v1 form. */
  label {
    color: var(--text);
  }
  /* The key buttons carry the v1 glyphs. */
  button {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
  }
  img {
    width: 16px;
    height: 16px;
    flex: none;
  }
</style>
