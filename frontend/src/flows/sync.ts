// Synchronization scenarios of spec §6.3: the connect check, Backup, Restore
// and the close question. They only talk to the user through Dialogs.
import type { Dialogs } from "../lib/dialogs.svelte";
import { parseError, type UIError } from "../lib/errors";

export type Counts = { added: number; updated: number; deleted: number };
export type BackupPreview = { counts: Counts; remoteNewer: boolean };
export type SyncState = { connected: boolean; dirty: boolean; restoreAvailable: boolean; busy: boolean };

/** SyncApi is what the scenarios need from SyncService and ConnectionService. */
export type SyncApi = {
  connect(): Promise<{ status: string; restore: Counts }>;
  prepareBackup(): Promise<BackupPreview>;
  commitBackup(): Promise<void>;
  prepareRestore(): Promise<Counts>;
  commitRestore(): Promise<void>;
  cancelPending(): Promise<void>;
  state(): Promise<SyncState>;
  trustHostKey(fingerprint: string): Promise<void>;
};

/** ConnectHooks are what the connect check may ask of the screen. */
export type ConnectHooks = {
  /** openSettings opens the settings after a wrong sync phrase. */
  openSettings?: () => void;
  /** restored runs after a Restore, to read the notes again. */
  restored?: () => void | Promise<void>;
};

const fresher = "It seems like your remote storage has fresher version of data.";

function lines(...parts: string[]): string {
  return parts.filter((p) => p !== "").join("\n");
}

function restoreQuestion(c: Counts): string {
  return lines("Would you like to restore data from remote storage?", c.deleted > 0 ? `Restore will delete ${c.deleted} local notes.` : "");
}

function backupQuestion(p: BackupPreview): string {
  return lines(
    p.remoteNewer ? `${fresher} Backup will overwrite it.` : "",
    "Would you like to save data on remote storage?",
    p.counts.deleted > 0 ? `Backup will delete ${p.counts.deleted} notes on the server.` : "",
  );
}

/** report shows err unless the user needs no message: a cancellation, or a busy app whose buttons are already off. */
async function report(d: Dialogs, err: UIError): Promise<void> {
  switch (err.code) {
    case "canceled":
    case "busy":
      return;
    case "host_key_mismatch":
      await d.error(
        lines(
          "The server host key does not match the trusted one.",
          `Expected: ${err.expected ?? ""}`,
          `Got: ${err.got ?? ""}`,
          "If the server key was changed on purpose, reset the trusted key in Settings.",
        ),
      );
      return;
    default:
      await d.error(err.message);
  }
}

/** trustQuestion asks about a server key that is not trusted yet. */
export function trustQuestion(err: UIError): string {
  return lines(
    `The authenticity of the server ${err.address ?? ""} cannot be established.`,
    `Its host key fingerprint is ${err.fingerprint ?? ""}.`,
    "Would you like to trust this key?",
  );
}

/**
 * connectFlow is scenario 1: it runs after unlocking and after OK in the
 * settings. An unknown server key is asked about once; a newer server is
 * offered for Restore in one question.
 */
export async function connectFlow(api: SyncApi, d: Dialogs, hooks: ConnectHooks = {}): Promise<void> {
  let out: { status: string; restore: Counts };
  try {
    out = await connectTrusting(api, d);
  } catch (e) {
    const err = parseError(e);
    await report(d, err);
    if (err.code === "wrong_sync_phrase") hooks.openSettings?.();
    return;
  }

  if (out.status !== "remote_newer") return;

  if (!(await d.ask(`${fresher} ${restoreQuestion(out.restore)}`))) {
    await api.cancelPending();
    return;
  }

  if (await commitRestore(api, d)) await hooks.restored?.();
}

/** connectTrusting connects; after an unknown server key it asks, trusts and connects once more. */
async function connectTrusting(api: SyncApi, d: Dialogs): Promise<{ status: string; restore: Counts }> {
  try {
    return await api.connect();
  } catch (e) {
    const err = parseError(e);
    if (err.code !== "host_key_unknown" || !err.fingerprint || !(await d.ask(trustQuestion(err)))) throw e;
    await api.trustHostKey(err.fingerprint);
    return await api.connect();
  }
}

/** backupFlow is scenario 2; it returns whether the Backup ran. */
export async function backupFlow(api: SyncApi, d: Dialogs): Promise<boolean> {
  try {
    const preview = await api.prepareBackup();
    if (!(await d.ask(backupQuestion(preview)))) {
      await api.cancelPending();
      return false;
    }
    await api.commitBackup();
  } catch (e) {
    await report(d, parseError(e));
    return false;
  }

  await d.info("Data successfully saved.");
  return true;
}

/** restoreFlow is scenario 3 started by the Restore button; it returns whether the Restore ran. */
export async function restoreFlow(api: SyncApi, d: Dialogs): Promise<boolean> {
  try {
    const counts = await api.prepareRestore();
    if (!(await d.ask(restoreQuestion(counts)))) {
      await api.cancelPending();
      return false;
    }
  } catch (e) {
    await report(d, parseError(e));
    return false;
  }

  return commitRestore(api, d);
}

async function commitRestore(api: SyncApi, d: Dialogs): Promise<boolean> {
  try {
    await api.commitRestore();
  } catch (e) {
    await report(d, parseError(e));
    return false;
  }

  await d.info("Data successfully restored.");
  return true;
}

/**
 * closeFlow is scenario 4: with unsaved changes and a connection it offers a
 * Backup, asking again only when the Backup deletes notes on the server or
 * overwrites newer data. A failed Backup asks "Close anyway?".
 */
export async function closeFlow(api: SyncApi, d: Dialogs, quit: () => Promise<void>): Promise<void> {
  const st = await api.state();
  if (!st.dirty || !st.connected || !(await d.ask("Would you like to save your data changes to remote database?"))) {
    await quit();
    return;
  }

  try {
    const preview = await api.prepareBackup();
    if ((preview.counts.deleted > 0 || preview.remoteNewer) && !(await d.ask(backupQuestion(preview)))) {
      await api.cancelPending();
      return;
    }
    await api.commitBackup();
  } catch (e) {
    await d.error(parseError(e).message);
    if (await d.ask("Close anyway?")) await quit();
    return;
  }

  await d.info("Data successfully synchronized.");
  await quit();
}
