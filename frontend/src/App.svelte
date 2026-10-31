<script lang="ts">
  // App picks the first screen: NewStorage when there is no local storage,
  // otherwise Unlock; both lead to Main.
  import { onMount } from "svelte";
  import MessageDialog from "./components/MessageDialog.svelte";
  import { VaultService } from "./lib/api";
  import { parseError } from "./lib/errors";
  import Main from "./screens/Main.svelte";
  import NewStorage from "./screens/NewStorage.svelte";
  import Unlock from "./screens/Unlock.svelte";

  type Screen = { kind: "loading" } | { kind: "new" } | { kind: "unlock" } | { kind: "main" } | { kind: "failed"; message: string };

  let screen: Screen = $state({ kind: "loading" });
  // Task 14 connects the synchronization state and the actions.
  const offline = { connected: false, dirty: false, restoreAvailable: false, busy: false };
  const notYet = () => {};

  onMount(async () => {
    try {
      screen = (await VaultService.Exists()) ? { kind: "unlock" } : { kind: "new" };
    } catch (err) {
      screen = { kind: "failed", message: parseError(err).message };
    }
  });
</script>

<main>
  {#if screen.kind === "new"}
    <NewStorage onCreated={() => (screen = { kind: "main" })} />
  {:else if screen.kind === "unlock"}
    <Unlock onUnlocked={() => (screen = { kind: "main" })} />
  {:else if screen.kind === "main"}
    <Main sync={offline} onSettings={notYet} onBackup={notYet} onRestore={notYet} />
  {:else if screen.kind === "failed"}
    <div class="card">
      <h1>Smartnote</h1>
      <p class="error">{screen.message}</p>
    </div>
  {/if}
</main>

<MessageDialog />
