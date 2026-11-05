<script lang="ts">
  // Settings edits the connection, the client SSH key and the trusted server
  // key (v1 UDlgConnection). The form holds the server key's fingerprint only;
  // a key trusted during Test, like a reset, is saved only with OK (spec §6.1).
  import { onMount } from "svelte";
  import exportIcon from "../assets/icons/export.png";
  import gearIcon from "../assets/icons/gear.png";
  import importIcon from "../assets/icons/import.png";
  import StrengthMeter from "../components/StrengthMeter.svelte";
  import YandexAccount from "../components/YandexAccount.svelte";
  import { ConnectionService, KeyService } from "../lib/api";
  import { dialogs } from "../lib/dialogs.svelte";
  import { parseError, type UIError } from "../lib/errors";
  import { openExternal, YANDEX_DOCS } from "../lib/external";
  import { emptySettings, fromForm, toForm, type SettingsForm, type SignInPhase } from "../lib/settings";
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
  let busy = $state("");
  let invalid = $state("");
  let passphrase: { value: string; retry: boolean } | undefined = $state();

  onMount(async () => {
    try {
      form = toForm(await ConnectionService.Load());
      loaded = true;
      hasKey = await KeyService.HasKey();
    } catch (e) {
      await dialogs.error(parseError(e).message);
    }
  });

  async function fail(e: unknown) {
    const err: UIError = parseError(e);
    invalid = err.code === "validation" ? (err.field ?? "") : "";
    if (err.code !== "canceled" && err.code !== "busy") await dialogs.error(err.message);
  }

  async function run(label: string, f: () => Promise<void>) {
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
    return run("Connecting…", async () => {
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
      await dialogs.info(empty ? "Connection successfully established. Remote storage is empty." : "Connection successfully established.");
    });
  }

  function save() {
    return run("Saving…", async () => {
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
    return run("Generating…", async () => {
      if (!allowed.generate) return;
      if (hasKey && !(await dialogs.ask("Are you sure you want to generate new key?\nCurrent key will be erased!"))) return;
      await KeyService.Generate();
      hasKey = true;
      await dialogs.info("New key successfully generated.");
    });
  }

  function exportPublic() {
    return run("Exporting…", async () => {
      const saved = await KeyService.ExportPublicToFile();
      await dialogs.info(saved ? "Public key was copied to the clipboard and saved to the file." : "Public key was copied to the clipboard.");
    });
  }

  function importKey(pass = "") {
    return run("Importing…", async () => {
      if (!allowed.import) return;
      try {
        if (!(await KeyService.ImportFromFile(pass))) return;
      } catch (e) {
        if (parseError(e).code !== "key_passphrase_required") throw e;
        passphrase = { value: "", retry: pass !== "" };
        return;
      }
      hasKey = true;
      await dialogs.info("Key successfully imported.");
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

  /** close abandons a running Yandex sign-in before the dialog goes away; the account block may already be gone. */
  async function close() {
    if (phase !== "idle") await ConnectionService.CancelSignIn().catch(() => {});
    onClose();
  }

  const phraseShort = $derived(form.syncPhrase !== "" && secretLen(form.syncPhrase) < LIMITS.phraseMin);
</script>

<div class="backdrop" use:modal>
  <form class="card settings" onsubmit={(e) => (e.preventDefault(), save())}>
    <h2>Settings</h2>

    <fieldset class="storage">
      <legend>Storage</legend>
      <div class="wide row">
        <label class="choice"><input type="radio" name="kind" value="none" bind:group={form.kind} disabled={phase !== "idle"} />None (local only)</label>
        <label class="choice"><input type="radio" name="kind" value="yandex_disk" bind:group={form.kind} disabled={phase !== "idle"} />Yandex Disk</label>
        <label class="choice"><input type="radio" name="kind" value="ssh_postgres" bind:group={form.kind} disabled={phase !== "idle"} />SSH + PostgreSQL</label>
      </div>
    </fieldset>

    {#if form.kind === "yandex_disk"}
      <fieldset class="yandex">
        <legend>Yandex account</legend>
        <div class="wide">
          <YandexAccount bind:form bind:phase bind:invalid disabled={!!busy} />
        </div>
        <details class="wide" open={invalid === "yandex_client_id" || undefined}>
          <summary>Advanced</summary>
          <label class="wide">
            Own client ID:
            <input aria-label="Own client ID" bind:value={form.yandexClientId} autocomplete="off" aria-invalid={invalid === "yandex_client_id"} />
          </label>
          <a href={YANDEX_DOCS} onclick={(e) => (e.preventDefault(), void openExternal(YANDEX_DOCS))}>How to register</a>
          <p class="muted">Data of another client ID is a separate, empty storage.</p>
        </details>
      </fieldset>
    {/if}

    <!-- SSH server and Database side by side, as in v1 UDlgConnection. -->
    {#if form.kind === "ssh_postgres"}
    <div class="pair">
      <fieldset>
        <legend>SSH server</legend>
        <label class="wide">Host <input bind:value={form.sshHost} aria-invalid={invalid === "ssh_host"} /></label>
        <label>Port <input bind:value={form.sshPort} inputmode="numeric" placeholder="22" aria-invalid={invalid === "ssh_port"} /></label>
        <label>User <input bind:value={form.sshUser} aria-invalid={invalid === "ssh_user"} /></label>
        <label>Password <input type="password" bind:value={form.sshPassword} autocomplete="off" /></label>
      </fieldset>

      <fieldset>
        <legend>Database</legend>
        <label>Host <input bind:value={form.dbHost} placeholder="127.0.0.1" aria-invalid={invalid === "db_host"} /></label>
        <label>Port <input bind:value={form.dbPort} inputmode="numeric" placeholder="5432" aria-invalid={invalid === "db_port"} /></label>
        <label>Name <input bind:value={form.dbName} aria-invalid={invalid === "db_name"} /></label>
        <label>User <input bind:value={form.dbUser} aria-invalid={invalid === "db_user"} /></label>
        <label>Password <input type="password" bind:value={form.dbPassword} autocomplete="off" /></label>
      </fieldset>
    </div>
    {/if}

    {#if form.kind !== "none"}
    <fieldset>
      <!-- The v1 group of the salt had the gear too. -->
      <legend><img src={gearIcon} alt="" />Sync phrase</legend>
      <label class="wide">
        Phrase (at least {LIMITS.phraseMin} characters, the same on every device)
        <input type="password" bind:value={form.syncPhrase} autocomplete="off" aria-invalid={invalid === "sync_phrase" || phraseShort} />
      </label>
      <div class="wide"><StrengthMeter phrase={form.syncPhrase} /></div>
    </fieldset>
    {/if}

    {#if form.kind === "ssh_postgres"}
    <fieldset>
      <legend>Keys</legend>
      <div class="wide row">
        <span class="muted">{hasKey === undefined ? "" : hasKey ? "Client key is set." : "No client key."}</span>
        <span class="spacer"></span>
        <button type="button" onclick={generate} disabled={!!busy || !allowed.generate}><img src={gearIcon} alt="" />Generate</button>
        <button type="button" onclick={exportPublic} disabled={!!busy || !allowed.export}><img src={exportIcon} alt="" />Export public</button>
        <button type="button" onclick={() => importKey()} disabled={!!busy || !allowed.import}><img src={importIcon} alt="" />Import</button>
      </div>
      <div class="wide row">
        <span class="muted" aria-invalid={invalid === "trusted_host_key"}>
          Server key: {form.trustedFingerprint || "not trusted yet"}
        </span>
        <span class="spacer"></span>
        <button type="button" onclick={resetTrust} disabled={!!busy || !form.trustedFingerprint}>Reset</button>
      </div>
    </fieldset>
    {/if}

    <!-- OK on the left, Test connection in the middle, Cancel on the right, as in v1. -->
    <div class="actions">
      <div class="row">
        <button type="submit" disabled={!!busy || !allowed.save}>OK</button>
        <span class="muted">{busy}</span>
      </div>
      <button type="button" onclick={test} disabled={!!busy || !allowed.test}>Test connection</button>
      <button class="cancel" type="button" onclick={close} disabled={!!busy}>Cancel</button>
    </div>
  </form>
</div>

{#if passphrase}
  <div class="backdrop top" use:modal>
    <form class="card" onsubmit={submitPassphrase}>
      <h2>Key passphrase</h2>
      <label>
        {passphrase.retry ? "Wrong passphrase. Enter the passphrase of the key" : "The key is protected. Enter its passphrase"}
        <!-- svelte-ignore a11y_autofocus -->
        <input type="password" bind:value={passphrase.value} autocomplete="off" autofocus />
      </label>
      <div class="buttons">
        <button type="submit" disabled={passphrase.value === ""}>OK</button>
        <button type="button" onclick={cancelPassphrase}>Cancel</button>
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
  fieldset {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 0.5rem 0.75rem;
    margin: 0;
    border: 1px solid var(--border);
    border-radius: 4px;
  }
  .wide {
    grid-column: 1 / -1;
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
  button,
  legend {
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
