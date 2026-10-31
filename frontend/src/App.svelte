<script lang="ts">
  // App picks the first screen: NewStorage when there is no local storage,
  // otherwise Unlock; both lead to Main.
  import { onMount } from "svelte";
  import MessageDialog from "./components/MessageDialog.svelte";
  import { VaultService } from "./lib/api";
  import { parseError } from "./lib/errors";
  import ChangePassword from "./screens/ChangePassword.svelte";
  import NewStorage from "./screens/NewStorage.svelte";
  import Unlock from "./screens/Unlock.svelte";

  type Screen = { kind: "loading" } | { kind: "new" } | { kind: "unlock" } | { kind: "main" } | { kind: "failed"; message: string };

  let screen: Screen = $state({ kind: "loading" });
  let changingPassword = $state(false);

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
    <!-- Main replaces this placeholder in task 13. -->
    <div class="card">
      <p>Local storage is unlocked.</p>
      <button onclick={() => (changingPassword = true)}>Change password</button>
    </div>
    {#if changingPassword}
      <ChangePassword onClose={() => (changingPassword = false)} />
    {/if}
  {:else if screen.kind === "failed"}
    <div class="card">
      <h1>Smartnote</h1>
      <p class="error">{screen.message}</p>
    </div>
  {/if}
</main>

<MessageDialog />
