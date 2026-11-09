// The overlay of the synchronization processes (Connect, Backup, Restore, the
// Backup on close): one for the whole process, not one per call to the server.
import type { Dialogs } from "./dialogs.svelte";
import { progressView, type Progress, type ProgressView } from "./progress";

/** ProcessKind names a synchronization process; the Backup on close is a "backup". */
export type ProcessKind = "connect" | "backup" | "restore";

const LABELS: Record<ProcessKind, string> = { connect: "Connecting…", backup: "Synchronizing…", restore: "Synchronizing…" };

/**
 * TRANSFERS is how many transfers report their progress in a process, by
 * storage; each fills an equal share of the bar. They follow the Go side
 * (usecase/sync.go): a Yandex Disk Backup reads the container in
 * PrepareBackup, reads it again when CommitBackup opens its session and then
 * writes it (blobremote/session.go); a server Backup reads the notes, then
 * writes them (postgres/session.go). CommitRestore transfers nothing. A
 * storage not listed has one transfer; more than planned stay in the last
 * share, as in the first Connect that creates the storage.
 */
const TRANSFERS: Record<ProcessKind, Record<string, number>> = {
  connect: {},
  backup: { yandex_disk: 3, ssh_postgres: 2 },
  restore: {},
};

/**
 * SyncOverlay is what the Overlay shows over the window. A process shows it
 * from its first call to the server until it ends: between the calls, and
 * under a question asked in between, it stays. A final message (info, warning
 * or error) hides it first, since the work is over by then. The bar is
 * shared by the transfers of the process (TRANSFERS).
 */
export class SyncOverlay {
  /** text is shown over the window; "" hides the overlay. */
  text = $state("");
  /** progress is the last transfer reported during the running process. */
  progress: Progress | undefined = $state();
  /** restoring is true from the Restore download the user started until the next call. */
  restoring = $state(false);

  /** kind is the running process; running counts the processes, as Close can start during a Backup. */
  private kind: ProcessKind = "backup";
  private running = 0;
  /**
   * step is the index of the transfer in the process, -1 before the first.
   * A transfer starts with the first report after the end of a call or in
   * another direction; a repeat in the same direction is the same transfer.
   */
  private step = $state(-1);
  private ended = true;

  /**
   * process runs flow as one process of kind. A progress left from another
   * call (Test in the settings) is dropped.
   */
  async process<T>(kind: ProcessKind, flow: () => Promise<T>): Promise<T> {
    if (this.running++ === 0) this.reset();
    this.kind = kind;
    try {
      return await flow();
    } finally {
      if (--this.running === 0) this.hide();
    }
  }

  /**
   * call runs a call to the server under the overlay; restore marks the
   * Restore download, until the next call, as its progress stays on screen.
   */
  call<T>(call: () => Promise<T>, restore = false): Promise<T> {
    this.text = LABELS[this.kind];
    this.restoring = restore;
    return call();
  }

  /**
   * follow takes a sync:progress event. The end of a call (direction "")
   * keeps the last progress on screen: the process goes on with its next call.
   */
  follow(p: Progress): void {
    if (p.direction === "") {
      this.ended = true;
      return;
    }
    if (this.ended || p.direction !== this.progress?.direction) this.step++;
    this.ended = false;
    this.progress = p;
  }

  /** view is the progress as the Overlay shows it, as a share of the bar of the whole process; undefined before a transfer. */
  view(): ProgressView | undefined {
    const p = this.progress;
    if (!p) return undefined;
    return progressView(p, this.restoring, { index: this.step, count: TRANSFERS[this.kind][p.storage] ?? 1 });
  }

  /** dialogs wraps d so that a final message hides the overlay; questions show over it. */
  dialogs(d: Dialogs): Dialogs {
    const final =
      (show: (msg: string) => Promise<void>) =>
      (msg: string): Promise<void> => {
        this.hide();
        return show(msg);
      };
    return {
      info: final((msg) => d.info(msg)),
      warning: final((msg) => d.warning(msg)),
      error: final((msg) => d.error(msg)),
      ask: (msg, labels) => d.ask(msg, labels),
      choose: (msg) => d.choose(msg),
    };
  }

  private hide(): void {
    this.text = "";
    this.reset();
  }

  private reset(): void {
    this.progress = undefined;
    this.restoring = false;
    this.step = -1;
    this.ended = true;
  }
}
