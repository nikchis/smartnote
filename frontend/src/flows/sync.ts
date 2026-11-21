// Synchronization scenarios of spec §6.3: the connect check, Backup, Restore
// and the close question. They only talk to the user through Dialogs.
import type { Dialogs } from "../lib/dialogs.svelte";
import { messageOf, parseError, type UIError } from "../lib/errors";
import { msg, type Msg } from "../lib/i18n.svelte";

export type Counts = { added: number; updated: number; deleted: number };
export type BackupPreview = { counts: Counts; remoteNewer: boolean };
export type RestorePreview = { counts: Counts; remoteOlder: boolean };
export type SyncState = { connected: boolean; dirty: boolean; restoreAvailable: boolean; busy: boolean };

/** SyncApi is what the scenarios need from SyncService and ConnectionService. */
export type SyncApi = {
  connect(): Promise<{ status: string; restore: Counts }>;
  prepareBackup(): Promise<BackupPreview>;
  commitBackup(): Promise<void>;
  prepareRestore(): Promise<RestorePreview>;
  commitRestore(): Promise<void>;
  cancelPending(): Promise<void>;
  state(): Promise<SyncState>;
  trustHostKey(fingerprint: string): Promise<void>;
};

/**
 * DiscardDraft asks whether the unsaved input of the note editor may be lost;
 * it is true when there is none or the user discards it.
 */
export type DiscardDraft = () => Promise<boolean>;

const noDraft: DiscardDraft = async () => true;

/** ConnectHooks are what the connect check may ask of the screen. */
export type ConnectHooks = {
  /** discardDraft runs after yes to a Restore, which replaces the editor's fields; false cancels the Restore. */
  discardDraft?: DiscardDraft;
  /** openSettings opens the settings after a wrong sync phrase or a refused Yandex sign-in. */
  openSettings?: () => void;
  /** restored runs after a Restore, to read the notes again. */
  restored?: () => void | Promise<void>;
};

/** lines are the messages one per line; an omitted line is left out. */
function lines(...parts: (Msg | undefined)[]): Msg {
  return { lines: parts.filter((p) => p !== undefined) };
}

/** restoreDeletes is the line about local notes a Restore deletes, if any. */
function restoreDeletes(c: Counts): Msg | undefined {
  return c.deleted > 0 ? msg("sync.restoreDeletes", { n: c.deleted }) : undefined;
}

function restoreQuestion(c: Counts, remoteOlder = false): Msg {
  return lines(remoteOlder ? msg("sync.olderPrefix") : undefined, msg("sync.restoreQuestion"), restoreDeletes(c));
}

/** needsBackupQuestion: a Backup is confirmed only when it deletes notes on the server or overwrites newer data. */
function needsBackupQuestion(p: BackupPreview): boolean {
  return p.counts.deleted > 0 || p.remoteNewer;
}

function backupQuestion(p: BackupPreview): Msg {
  return lines(
    p.remoteNewer ? msg("sync.fresherBackup") : undefined,
    msg("sync.backupQuestion"),
    p.counts.deleted > 0 ? msg("sync.backupDeletes", { n: p.counts.deleted }) : undefined,
  );
}

/** errorText is the message shown for err; a changed server key shows both fingerprints. */
function errorText(err: UIError): Msg {
  if (err.code !== "host_key_mismatch") return messageOf(err);
  return msg("sync.hostKeyMismatch", { expected: err.expected ?? "", got: err.got ?? "" });
}

/** report shows err unless the user needs no message: a cancellation, or a busy app whose buttons are already off. */
async function report(d: Dialogs, err: UIError): Promise<void> {
  if (err.code === "canceled" || err.code === "busy") return;
  await d.error(errorText(err));
}

/** trustQuestion asks about a server key that is not trusted yet. */
export function trustQuestion(err: UIError): Msg {
  return msg("sync.trustQuestion", { address: err.address ?? "", fingerprint: err.fingerprint ?? "" });
}

/**
 * connectFlow is scenario 1: it runs after unlocking and after OK in the
 * settings. An unknown server key is asked about once; a newer server is
 * offered for Restore in one question; an older one only gets a warning.
 */
export async function connectFlow(api: SyncApi, d: Dialogs, hooks: ConnectHooks = {}): Promise<void> {
  let out: { status: string; restore: Counts };
  try {
    out = await connectTrusting(api, d);
  } catch (e) {
    const err = parseError(e);
    await report(d, err);
    if (err.code === "wrong_sync_phrase" || err.code === "reauth_required") hooks.openSettings?.();
    return;
  }

  if (out.status === "remote_older") {
    await d.warning(msg("sync.olderWarning"));
    return;
  }

  if (out.status !== "remote_newer") return;

  if (!(await d.ask(lines(msg("sync.fresherRestore"), restoreDeletes(out.restore)))) || !(await (hooks.discardDraft ?? noDraft)())) {
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

/**
 * backupFlow is scenario 2; it returns whether the Backup ran. It asks only
 * when the Backup deletes notes on the server or overwrites newer data.
 */
export async function backupFlow(api: SyncApi, d: Dialogs): Promise<boolean> {
  try {
    const preview = await api.prepareBackup();
    if (needsBackupQuestion(preview) && !(await d.ask(backupQuestion(preview)))) {
      await api.cancelPending();
      return false;
    }
    await api.commitBackup();
  } catch (e) {
    await report(d, parseError(e));
    return false;
  }

  await d.info(msg("sync.saved"));
  return true;
}

/**
 * restoreFlow is scenario 3 started by the Restore button; it returns whether
 * the Restore ran. After yes, discardDraft may still keep the editor's draft
 * and cancel the Restore.
 */
export async function restoreFlow(api: SyncApi, d: Dialogs, discardDraft: DiscardDraft = noDraft): Promise<boolean> {
  try {
    const preview = await api.prepareRestore();
    if (!(await d.ask(restoreQuestion(preview.counts, preview.remoteOlder))) || !(await discardDraft())) {
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

  await d.info(msg("sync.restored"));
  return true;
}

/**
 * closeFlow is scenario 4. An unsaved draft of the editor is asked about
 * first; keeping it keeps the window open. Then, with unsaved changes and a
 * connection, it offers a Backup, asking again only when the Backup deletes
 * notes on the server or overwrites newer data. Cancel on the offer keeps the
 * window open. A failed Backup asks "Close anyway?".
 */
export async function closeFlow(api: SyncApi, d: Dialogs, quit: () => Promise<void>, discardDraft: DiscardDraft = noDraft): Promise<void> {
  if (!(await discardDraft())) return;

  const st = await api.state();
  if (!st.dirty || !st.connected) {
    await quit();
    return;
  }

  const save = await d.choose(msg("sync.closeQuestion"));
  if (save === "cancel") return;
  if (save === "no") {
    await quit();
    return;
  }

  try {
    const preview = await api.prepareBackup();
    if (needsBackupQuestion(preview) && !(await d.ask(backupQuestion(preview)))) {
      await api.cancelPending();
      return;
    }
    await api.commitBackup();
  } catch (e) {
    await d.error(errorText(parseError(e)));
    if (await d.ask(msg("sync.closeAnyway"))) await quit();
    return;
  }

  await d.info(msg("sync.synchronized"));
  await quit();
}
