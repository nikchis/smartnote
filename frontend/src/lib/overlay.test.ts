import { beforeEach, describe, expect, it } from "vitest";
import { backupFlow, closeFlow, restoreFlow, type SyncApi } from "../flows/sync";
import type { Dialogs } from "./dialogs.svelte";
import { SyncOverlay } from "./overlay.svelte";
import type { Progress } from "./progress";

const zero = { added: 0, updated: 0, deleted: 0 };
const upload: Progress = { storage: "yandex_disk", direction: "upload", unit: "bytes", done: 5, total: 10 };
const end: Progress = { storage: "", direction: "", unit: "", done: 0, total: 0 };
const disk = (direction: string, done: number, total: number): Progress => ({ storage: "yandex_disk", direction, unit: "bytes", done, total });
const server = (direction: string, done: number, total: number): Progress => ({ storage: "ssh_postgres", direction, unit: "notes", done, total });

/** RecordingDialogs notes each dialog with the overlay text shown under it ("" when hidden). */
class RecordingDialogs implements Dialogs {
  log: string[] = [];
  constructor(
    private readonly overlay: SyncOverlay,
    private readonly answers: boolean[] = [],
  ) {}
  private note(kind: string, msg: string) {
    this.log.push(`${kind} ${msg} [overlay: ${this.overlay.text}]`);
  }
  async info(msg: string) {
    this.note("info", msg);
  }
  async warning(msg: string) {
    this.note("warning", msg);
  }
  async error(msg: string) {
    this.note("error", msg);
  }
  async ask(msg: string) {
    this.note("ask", msg);
    return this.answers.shift() === true;
  }
  async choose(msg: string) {
    this.note("choose", msg);
    return "yes" as const;
  }
}

/** recordingApi is a SyncApi whose calls go through overlay and note the overlay text seen while each runs. */
function recordingApi(overlay: SyncOverlay, seen: string[], fail: Partial<Record<keyof SyncApi, Error>> = {}): SyncApi {
  const call = <T>(name: keyof SyncApi, result: T, restore = false) =>
    overlay.call(async () => {
      // A call to the server ends with the end of its progress (ProgressFeed.End in Go).
      seen.push(`${name} [overlay: ${overlay.text}]`);
      overlay.follow(end);
      const err = fail[name];
      if (err) throw err;
      return result;
    }, restore);
  return {
    connect: () => call("connect", { status: "in_sync", restore: zero }),
    prepareBackup: () => call("prepareBackup", { counts: zero, remoteNewer: false }),
    commitBackup: () => call("commitBackup", undefined),
    prepareRestore: () => call("prepareRestore", { counts: zero, remoteOlder: false }, true),
    commitRestore: () => call("commitRestore", undefined),
    cancelPending: async () => void seen.push("cancelPending"),
    state: async () => ({ connected: true, dirty: true, restoreAvailable: true, busy: false }),
    trustHostKey: async () => undefined,
  };
}

let overlay: SyncOverlay;

beforeEach(() => {
  overlay = new SyncOverlay();
});

describe("SyncOverlay", () => {
  it("shows nothing before the first call to the server and hides after the process", async () => {
    const seen: string[] = [];

    await overlay.process("backup", async () => {
      seen.push(`before [overlay: ${overlay.text}]`);
      await overlay.call(async () => seen.push(`call [overlay: ${overlay.text}]`));
    });

    expect(seen).toEqual(["before [overlay: ]", "call [overlay: Synchronizing…]"]);
    expect(overlay.text).toBe("");
  });

  it("stays shown between the calls of one process", async () => {
    const seen: string[] = [];
    const d = new RecordingDialogs(overlay);

    await overlay.process("backup", () => backupFlow(recordingApi(overlay, seen), overlay.dialogs(d)));

    expect(seen).toEqual(["prepareBackup [overlay: Synchronizing…]", "commitBackup [overlay: Synchronizing…]"]);
  });

  it("stays under a question in the middle of a Restore and hides for the final message", async () => {
    const seen: string[] = [];
    const d = new RecordingDialogs(overlay, [true]);

    await overlay.process("restore", () => restoreFlow(recordingApi(overlay, seen), overlay.dialogs(d)));

    expect(seen).toEqual(["prepareRestore [overlay: Synchronizing…]", "commitRestore [overlay: Synchronizing…]"]);
    expect(d.log).toEqual([
      "ask Would you like to restore data from remote storage? [overlay: Synchronizing…]",
      "info Data successfully restored. [overlay: ]",
    ]);
    expect(overlay.text).toBe("");
  });

  it("hides for an error and after a process that throws", async () => {
    const d = new RecordingDialogs(overlay);
    const fail = new Error(JSON.stringify({ code: "cloud", message: "Yandex Disk error." }));

    await overlay.process("backup", () => backupFlow(recordingApi(overlay, [], { commitBackup: fail }), overlay.dialogs(d)));
    expect(d.log).toEqual(["error Yandex Disk error. [overlay: ]"]);

    await expect(overlay.process("backup", () => overlay.call(() => Promise.reject(fail)))).rejects.toBe(fail);
    expect(overlay.text).toBe("");
  });

  it("keeps the last progress through the end of a call until the process ends", async () => {
    await overlay.process("backup", async () => {
      await overlay.call(async () => {
        overlay.follow(upload);
        overlay.follow(end);
      });
      expect(overlay.progress).toEqual(upload);
    });

    expect(overlay.progress).toBeUndefined();
  });

  it("drops a progress left from before the process", async () => {
    overlay.follow(upload);

    await overlay.process("backup", async () => {
      await overlay.call(async () => expect(overlay.progress).toBeUndefined());
    });
  });

  it("marks the Restore download from its call until the next one", async () => {
    const restoring: boolean[] = [];

    await overlay.process("backup", async () => {
      await overlay.call(async () => restoring.push(overlay.restoring), true);
      // The question of the Restore shows over the progress of the download.
      restoring.push(overlay.restoring);
      await overlay.call(async () => restoring.push(overlay.restoring));
    });

    expect(restoring).toEqual([true, true, false]);
    expect(overlay.restoring).toBe(false);
  });

  it("stays shown while another process still runs", async () => {
    let finishFirst!: () => void;
    const first = overlay.process("backup", () => overlay.call(() => new Promise<void>((r) => (finishFirst = r))));

    await overlay.process("backup", () => overlay.call(async () => undefined));
    expect(overlay.text).toBe("Synchronizing…");

    finishFirst();
    await first;
    expect(overlay.text).toBe("");
  });

  it("shows the text of the process until a transfer reports its progress", async () => {
    const texts: string[] = [];

    await overlay.process("connect", () => overlay.call(async () => texts.push(overlay.text)));
    await overlay.process("restore", () => overlay.call(async () => texts.push(overlay.text)));

    expect(texts).toEqual(["Connecting…", "Synchronizing…"]);
  });

  it("splits the bar of a Yandex Disk Backup into its three transfers", async () => {
    const seen: string[] = [];
    const note = () => {
      const v = overlay.view();
      seen.push(`${v?.detail} = ${v?.value.toFixed(3)}`);
    };

    await overlay.process("backup", async () => {
      await overlay.call(async () => {
        // PrepareBackup reads the container.
        overlay.follow(disk("download", 0, 1_000_000));
        note();
        overlay.follow(disk("download", 1_000_000, 1_000_000));
        note();
        overlay.follow(end);
      });
      await overlay.call(async () => {
        // CommitBackup reads it once more, then writes the new one.
        overlay.follow(disk("download", 500_000, 1_000_000));
        note();
        overlay.follow(disk("upload", 0, 2_000_000));
        note();
        overlay.follow(disk("upload", 2_000_000, 2_000_000));
        note();
        overlay.follow(end);
      });
      note();
    });

    expect(seen).toEqual([
      "Step 1 of 3 · 0.0 of 1.0 MB · 0% = 0.000",
      "Step 1 of 3 · 1.0 of 1.0 MB · 33% = 0.333",
      "Step 2 of 3 · 0.5 of 1.0 MB · 50% = 0.500",
      "Step 3 of 3 · 0.0 of 2.0 MB · 66% = 0.667",
      "Step 3 of 3 · Finishing… = 1.000",
      "Step 3 of 3 · Finishing… = 1.000",
    ]);
  });

  it("splits the bar of a server Backup into its two transfers", async () => {
    const seen: (string | undefined)[] = [];

    await overlay.process("backup", async () => {
      await overlay.call(async () => {
        overlay.follow(server("download", 170, 340));
        seen.push(overlay.view()?.detail);
        overlay.follow(end);
      });
      await overlay.call(async () => {
        overlay.follow(server("upload", 170, 340));
        seen.push(overlay.view()?.detail);
      });
    });

    expect(seen).toEqual(["Step 1 of 2 · 170 of 340 notes · 25%", "Step 2 of 2 · 170 of 340 notes · 75%"]);
  });

  it("fills the whole bar with the one transfer of a Restore", async () => {
    let detail: string | undefined;

    await overlay.process("restore", () =>
      overlay.call(async () => {
        overlay.follow(disk("download", 250_000, 1_000_000));
        detail = overlay.view()?.detail;
      }, true),
    );

    expect(detail).toBe("0.3 of 1.0 MB · 25%");
  });

  it("refills the same share when a transfer starts over", async () => {
    const seen: (string | undefined)[] = [];

    await overlay.process("backup", () =>
      overlay.call(async () => {
        overlay.follow(disk("download", 800_000, 1_000_000));
        // The download of the container is repeated once (yadisk Store.Read).
        overlay.follow(disk("download", 0, 1_000_000));
        seen.push(overlay.view()?.detail);
      }),
    );

    expect(seen).toEqual(["Step 1 of 3 · 0.0 of 1.0 MB · 0%"]);
  });

  it("keeps a transfer beyond the planned ones in the last share", async () => {
    let detail: string | undefined;

    await overlay.process("connect", () =>
      overlay.call(async () => {
        // The first Connect creates the container: an upload, a read and the first Backup.
        overlay.follow(disk("upload", 1, 1));
        overlay.follow(disk("download", 1, 1));
        overlay.follow(disk("upload", 1, 2));
        detail = overlay.view()?.detail;
      }),
    );

    expect(detail).toBe("0.0 of 0.0 MB · 50%");
  });

  it("starts counting the transfers anew in each process", async () => {
    await overlay.process("backup", () => overlay.call(async () => overlay.follow(server("download", 1, 2))));
    let detail: string | undefined;

    await overlay.process("backup", () =>
      overlay.call(async () => {
        overlay.follow(server("download", 1, 2));
        detail = overlay.view()?.detail;
      }),
    );

    expect(detail).toBe("Step 1 of 2 · 1 of 2 notes · 25%");
  });

  describe("while another process runs", () => {
    /** busy is a call that Go refuses during another operation: it ends with the end of the progress, then fails. */
    const busy = () =>
      overlay.call(async () => {
        overlay.follow(end);
        throw new Error(JSON.stringify({ code: "busy", message: "Another operation with the remote storage is in progress." }));
      });

    /** running starts a process whose one call stays in a transfer until finish is called. */
    function running(kind: "backup" | "restore", first: Progress, restore = false) {
      let finish!: () => void;
      const done = overlay.process(kind, () =>
        overlay.call(() => {
          overlay.follow(first);
          return new Promise<void>((r) => (finish = r));
        }, restore),
      );
      return { done, finish: () => finish() };
    }

    it("keeps the overlay and the progress of a running Backup when the Backup on close fails", async () => {
      const backup = running("backup", disk("upload", 1_000_000, 2_000_000));
      const d = new RecordingDialogs(overlay);

      let quit = false;

      // Close asks to save, Yes; its Backup is refused, and No to "Close anyway?" keeps the window.
      await overlay.process("backup", () =>
        closeFlow({ ...recordingApi(overlay, []), prepareBackup: busy }, overlay.dialogs(d), async () => void (quit = true)),
      );

      expect(d.log).toEqual([
        "choose Would you like to save your data changes to remote storage? [overlay: Synchronizing…]",
        "error Another operation with the remote storage is in progress. [overlay: Synchronizing…]",
        "ask Close anyway? [overlay: Synchronizing…]",
      ]);
      expect(quit).toBe(false);
      expect(overlay.text).toBe("Synchronizing…");
      expect(overlay.view()?.detail).toBe("Step 1 of 3 · 1.0 of 2.0 MB · 16%");

      backup.finish();
      await backup.done;
      expect(overlay.text).toBe("");
    });

    it("keeps the share of a transfer through the end of a refused call", async () => {
      const backup = running("backup", disk("download", 400_000, 1_000_000));

      await overlay.process("backup", busy).catch(() => undefined);
      overlay.follow(disk("download", 500_000, 1_000_000));

      expect(overlay.view()?.detail).toBe("Step 1 of 3 · 0.5 of 1.0 MB · 16%");
      backup.finish();
      await backup.done;
    });

    it("keeps the title and the shares of a running Restore", async () => {
      const restore = running("restore", disk("download", 250_000, 1_000_000), true);

      await overlay.process("backup", busy).catch(() => undefined);
      overlay.follow(disk("download", 500_000, 1_000_000));

      expect(overlay.view()).toMatchObject({ title: "Downloading from Yandex Disk…", detail: "0.5 of 1.0 MB · 50%" });
      restore.finish();
      await restore.done;
    });

    it("counts the transfers of the process that goes on after the first one ends", async () => {
      const restore = running("restore", disk("download", 1_000_000, 1_000_000), true);
      let goOn!: () => void;
      const close = overlay.process("backup", async () => {
        await new Promise<void>((r) => (goOn = r));
        await overlay.call(async () => {
          overlay.follow(disk("download", 0, 1_000_000));
          expect(overlay.view()).toMatchObject({ title: "Synchronizing with Yandex Disk…", detail: "Step 1 of 3 · 0.0 of 1.0 MB · 0%" });
        });
      });

      restore.finish();
      await restore.done;
      expect(overlay.text).toBe("Synchronizing…");
      goOn();
      await close;
      expect(overlay.text).toBe("");
    });
  });
});

