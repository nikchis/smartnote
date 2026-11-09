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
  import { SyncOverlay } from "./lib/overlay.svelte";
  import Main from "./screens/Main.svelte";
  import NewStorage from "./screens/NewStorage.svelte";
  import Settings from "./screens/Settings.svelte";
  import Unlock from "./screens/Unlock.svelte";

  type Screen = { kind: "loading" } | { kind: "new" } | { kind: "unlock" } | { kind: "main" } | { kind: "failed"; message: string };

  let screen: Screen = $state({ kind: "loading" });
  let sync: StateDTO = $state({ connected: false, dirty: false, restoreAvailable: false, busy: false });
  /** overlay covers the window for the whole of a synchronization process, across its calls to the server. */
  const overlay = new SyncOverlay();
  let showSettings = $state(false);
  let closing = false;
  let main: ReturnType<typeof Main> | undefined = $state();

  /** syncApi is SyncService for the flows, every call to the server under the overlay. */
  const syncApi: SyncApi = {
    connect: () => overlay.call(() => SyncService.Connect()),
    prepareBackup: () => overlay.call(() => SyncService.PrepareBackup()),
    commitBackup: () => overlay.call(() => SyncService.CommitBackup()),
    prepareRestore: () => overlay.call(() => SyncService.PrepareRestore(), true),
    commitRestore: () => overlay.call(() => SyncService.CommitRestore()),
    cancelPending: () => SyncService.CancelPending(),
    state: () => SyncService.State(),
    trustHostKey: (fingerprint) => ConnectionService.TrustHostKey(fingerprint),
  };
  /** syncDialogs hide the overlay for the final message of a process. */
  const syncDialogs = overlay.dialogs(dialogs);

  const reload = () => main?.reload();
  // Without the Main screen there is no editor, so no draft.
  const discardDraft = async () => (main ? main.discardDraft() : true);

  async function connect() {
    await overlay.process("connect", () =>
      connectFlow(syncApi, syncDialogs, { openSettings: () => (showSettings = true), discardDraft, restored: reload }),
    );
  }

  async function unlocked() {
    screen = { kind: "main" };
    await connect();
  }

  async function backup() {
    await overlay.process("backup", () => backupFlow(syncApi, syncDialogs));
  }

  async function restore() {
    if (await overlay.process("restore", () => restoreFlow(syncApi, syncDialogs, discardDraft))) await reload();
  }

  async function settingsSaved() {
    showSettings = false;
    await connect();
  }

  async function closeRequested() {
    if (closing) return;
    closing = true;
    try {
      await overlay.process("backup", () => closeFlow(syncApi, syncDialogs, () => AppService.ConfirmQuit(), discardDraft));
    } catch (e) {
      await dialogs.error(parseError(e).message);
    } finally {
      closing = false;
    }
  }

  onMount(() => {
    const offState = Events.On("sync:state", (ev) => (sync = ev.data));
    const offProgress = Events.On("sync:progress", (ev) => overlay.follow(ev.data));
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
      offProgress();
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

{#if overlay.text}
  <Overlay text={overlay.text} progress={overlay.view()} />
{/if}

<MessageDialog />
