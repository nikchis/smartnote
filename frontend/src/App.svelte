<script lang="ts">
  // App picks the first screen (NewStorage or Unlock, then Main), runs the
  // synchronization scenarios with their overlays and the close scenario.
  import { Events } from "@wailsio/runtime";
  import { onMount } from "svelte";
  import MessageDialog from "./components/MessageDialog.svelte";
  import Overlay from "./components/Overlay.svelte";
  import { backupFlow, closeFlow, connectFlow, restoreFlow, type SyncApi } from "./flows/sync";
  import { AppService, ConnectionService, SyncService, VaultService, type StateDTO } from "./lib/api";
  import { dialogs } from "./lib/dialogs.svelte";
  import { parseError } from "./lib/errors";
  import Main from "./screens/Main.svelte";
  import NewStorage from "./screens/NewStorage.svelte";
  import Settings from "./screens/Settings.svelte";
  import Unlock from "./screens/Unlock.svelte";

  type Screen = { kind: "loading" } | { kind: "new" } | { kind: "unlock" } | { kind: "main" } | { kind: "failed"; message: string };

  let screen: Screen = $state({ kind: "loading" });
  let sync: StateDTO = $state({ connected: false, dirty: false, restoreAvailable: false, busy: false });
  let overlay = $state("");
  let showSettings = $state(false);
  let closing = false;
  let main: ReturnType<typeof Main> | undefined = $state();

  /** withOverlay shows text over the window while a call to the server runs. */
  async function withOverlay<T>(text: string, call: () => Promise<T>): Promise<T> {
    overlay = text;
    try {
      return await call();
    } finally {
      overlay = "";
    }
  }

  function syncApi(text: string): SyncApi {
    return {
      connect: () => withOverlay(text, () => SyncService.Connect()),
      prepareBackup: () => withOverlay(text, () => SyncService.PrepareBackup()),
      commitBackup: () => withOverlay(text, () => SyncService.CommitBackup()),
      prepareRestore: () => withOverlay(text, () => SyncService.PrepareRestore()),
      commitRestore: () => withOverlay(text, () => SyncService.CommitRestore()),
      cancelPending: () => SyncService.CancelPending(),
      state: () => SyncService.State(),
      trustHostKey: (fingerprint) => ConnectionService.TrustHostKey(fingerprint),
    };
  }

  const reload = () => main?.reload();

  async function connect() {
    await connectFlow(syncApi("Connecting…"), dialogs, { openSettings: () => (showSettings = true), restored: reload });
  }

  async function unlocked() {
    screen = { kind: "main" };
    await connect();
  }

  async function backup() {
    await backupFlow(syncApi("Synchronizing…"), dialogs);
  }

  async function restore() {
    if (await restoreFlow(syncApi("Synchronizing…"), dialogs)) await reload();
  }

  async function settingsSaved() {
    showSettings = false;
    await connect();
  }

  async function closeRequested() {
    if (closing) return;
    closing = true;
    try {
      await closeFlow(syncApi("Synchronizing…"), dialogs, () => AppService.ConfirmQuit());
    } catch (e) {
      await dialogs.error(parseError(e).message);
    } finally {
      closing = false;
    }
  }

  onMount(() => {
    const offState = Events.On("sync:state", (ev) => (sync = ev.data));
    // The close scenario runs on every screen; before unlocking it just quits.
    const offClose = Events.On("app:close-requested", () => void closeRequested());
    // Until this call Go closes the window without asking: nothing listened yet.
    void AppService.Ready();

    (async () => {
      try {
        sync = await SyncService.State();
        screen = (await VaultService.Exists()) ? { kind: "unlock" } : { kind: "new" };
      } catch (err) {
        screen = { kind: "failed", message: parseError(err).message };
      }
    })();

    return () => {
      offState();
      offClose();
    };
  });
</script>

<main>
  {#if screen.kind === "new"}
    <NewStorage onCreated={unlocked} />
  {:else if screen.kind === "unlock"}
    <Unlock onUnlocked={unlocked} />
  {:else if screen.kind === "main"}
    <Main bind:this={main} {sync} onSettings={() => (showSettings = true)} onBackup={backup} onRestore={restore} />
    {#if showSettings}
      <Settings onSaved={settingsSaved} onClose={() => (showSettings = false)} />
    {/if}
  {:else if screen.kind === "failed"}
    <div class="card">
      <h1>Smartnote</h1>
      <p class="error">{screen.message}</p>
    </div>
  {/if}
</main>

{#if overlay}
  <Overlay text={overlay} />
{/if}

<MessageDialog />
