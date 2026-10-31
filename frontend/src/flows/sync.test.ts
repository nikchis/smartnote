import { beforeEach, describe, expect, it } from "vitest";
import type { Dialogs } from "../lib/dialogs.svelte";
import { backupFlow, closeFlow, connectFlow, restoreFlow, type BackupPreview, type Counts, type SyncApi, type SyncState } from "./sync";

const zero: Counts = { added: 0, updated: 0, deleted: 0 };

function uiError(code: string, message: string, extra: Record<string, string> = {}): Error {
  return new Error(JSON.stringify({ code, message, ...extra }));
}

class FakeApi implements SyncApi {
  calls: string[] = [];
  connectResults: Array<{ status: string; restore: Counts } | Error> = [{ status: "in_sync", restore: zero }];
  preview: BackupPreview | Error = { counts: zero, remoteNewer: false };
  restoreCounts: Counts | Error = zero;
  commitBackupError?: Error;
  commitRestoreError?: Error;
  st: SyncState = { connected: true, dirty: false, restoreAvailable: false, busy: false };

  async connect() {
    this.calls.push("connect");
    const r = this.connectResults.shift() ?? { status: "in_sync", restore: zero };
    if (r instanceof Error) throw r;
    return r;
  }
  async prepareBackup() {
    this.calls.push("prepareBackup");
    if (this.preview instanceof Error) throw this.preview;
    return this.preview;
  }
  async commitBackup() {
    this.calls.push("commitBackup");
    if (this.commitBackupError) throw this.commitBackupError;
  }
  async prepareRestore() {
    this.calls.push("prepareRestore");
    if (this.restoreCounts instanceof Error) throw this.restoreCounts;
    return this.restoreCounts;
  }
  async commitRestore() {
    this.calls.push("commitRestore");
    if (this.commitRestoreError) throw this.commitRestoreError;
  }
  async cancelPending() {
    this.calls.push("cancelPending");
  }
  async state() {
    return this.st;
  }
  async trustHostKey(fingerprint: string) {
    this.calls.push(`trustHostKey ${fingerprint}`);
  }
}

class FakeDialogs implements Dialogs {
  log: string[] = [];
  /** answers for ask and choose in order; true is Yes, false is No. */
  answers: (boolean | "cancel")[] = [];
  async info(msg: string) {
    this.log.push(`info: ${msg}`);
  }
  async error(msg: string) {
    this.log.push(`error: ${msg}`);
  }
  async ask(msg: string) {
    this.log.push(`ask: ${msg}`);
    return this.answers.shift() === true;
  }
  async choose(msg: string) {
    this.log.push(`ask: ${msg}`);
    const a = this.answers.shift() ?? false;
    return a === "cancel" ? "cancel" : a ? "yes" : "no";
  }
  questions() {
    return this.log.filter((l) => l.startsWith("ask: ")).map((l) => l.slice(5));
  }
}

let api: FakeApi;
let d: FakeDialogs;

beforeEach(() => {
  api = new FakeApi();
  d = new FakeDialogs();
});

describe("connectFlow", () => {
  it("remote_newer asks one question with the deletion line; yes restores", async () => {
    api.connectResults = [{ status: "remote_newer", restore: { added: 0, updated: 1, deleted: 2 } }];
    d.answers = [true];
    let restored = 0;

    await connectFlow(api, d, { restored: () => void restored++ });

    expect(d.questions()).toHaveLength(1);
    expect(d.questions()[0].startsWith("It seems like your remote storage has fresher version of data.")).toBe(true);
    expect(d.questions()[0]).toContain("Restore will delete 2 local notes.");
    expect(api.calls).toContain("commitRestore");
    expect(d.log).toContain("info: Data successfully restored.");
    expect(restored).toBe(1);
  });

  it("remote_newer without deletions has no deletion line", async () => {
    api.connectResults = [{ status: "remote_newer", restore: { added: 1, updated: 0, deleted: 0 } }];

    await connectFlow(api, d);

    expect(d.questions()).toEqual([
      "It seems like your remote storage has fresher version of data. Would you like to restore data from remote storage?",
    ]);
  });

  it("remote_newer and no cancels the pending Restore", async () => {
    api.connectResults = [{ status: "remote_newer", restore: zero }];
    d.answers = [false];

    await connectFlow(api, d);

    expect(api.calls).toEqual(["connect", "cancelPending"]);
  });

  it("host_key_unknown asks with the fingerprint; yes trusts and connects again", async () => {
    api.connectResults = [
      uiError("host_key_unknown", "unknown server host key SHA256:abc", { fingerprint: "SHA256:abc", address: "example.org:22" }),
      { status: "in_sync", restore: zero },
    ];
    d.answers = [true];

    await connectFlow(api, d);

    expect(d.questions()[0]).toContain("SHA256:abc");
    expect(d.questions()[0]).toContain("example.org:22");
    expect(api.calls).toEqual(["connect", "trustHostKey SHA256:abc", "connect"]);
  });

  it("host_key_unknown and no does not trust", async () => {
    api.connectResults = [uiError("host_key_unknown", "m", { fingerprint: "SHA256:abc", address: "h:22" })];

    await connectFlow(api, d);

    expect(api.calls).toEqual(["connect"]);
  });

  it("wrong_sync_phrase shows the error and opens the settings", async () => {
    api.connectResults = [uiError("wrong_sync_phrase", "The sync phrase does not match the remote storage.")];
    let opened = 0;

    await connectFlow(api, d, { openSettings: () => void opened++ });

    expect(d.log).toEqual(["error: The sync phrase does not match the remote storage."]);
    expect(opened).toBe(1);
  });

  it("host_key_mismatch shows both fingerprints", async () => {
    api.connectResults = [uiError("host_key_mismatch", "m", { expected: "SHA256:old", got: "SHA256:new" })];

    await connectFlow(api, d);

    expect(d.log[0]).toContain("SHA256:old");
    expect(d.log[0]).toContain("SHA256:new");
  });

  it("canceled and busy show nothing", async () => {
    api.connectResults = [uiError("canceled", "m")];
    await connectFlow(api, d);
    api.connectResults = [uiError("busy", "m")];
    await connectFlow(api, d);

    expect(d.log).toEqual([]);
  });

  it("other errors are shown", async () => {
    api.connectResults = [uiError("ssh", "ssh: connect example.org:22: connection refused")];

    await connectFlow(api, d);

    expect(d.log).toEqual(["error: ssh: connect example.org:22: connection refused"]);
  });
});

describe("backupFlow", () => {
  it("asks without a deletion line and reports success", async () => {
    d.answers = [true];

    expect(await backupFlow(api, d)).toBe(true);
    expect(d.questions()).toEqual(["Would you like to save data on remote storage?"]);
    expect(d.log).toContain("info: Data successfully saved.");
  });

  it("adds the deletion line", async () => {
    api.preview = { counts: { added: 0, updated: 0, deleted: 3 }, remoteNewer: false };

    await backupFlow(api, d);

    expect(d.questions()).toEqual(["Would you like to save data on remote storage?\nBackup will delete 3 notes on the server."]);
  });

  it("warns first when the server is newer", async () => {
    api.preview = { counts: zero, remoteNewer: true };

    await backupFlow(api, d);

    expect(d.questions()[0].startsWith("It seems like your remote storage has fresher version of data. Backup will overwrite it.")).toBe(true);
    expect(d.questions()[0]).toContain("Would you like to save data on remote storage?");
  });

  it("no cancels the pending Backup", async () => {
    expect(await backupFlow(api, d)).toBe(false);
    expect(api.calls).toEqual(["prepareBackup", "cancelPending"]);
  });

  it("remote_changed shows its message", async () => {
    d.answers = [true];
    api.commitBackupError = uiError("remote_changed", "Remote data changed during the operation. Please try again.");

    expect(await backupFlow(api, d)).toBe(false);
    expect(d.log).toContain("error: Remote data changed during the operation. Please try again.");
  });
});

describe("restoreFlow", () => {
  it("asks, restores and reports success", async () => {
    api.restoreCounts = { added: 1, updated: 0, deleted: 4 };
    d.answers = [true];

    expect(await restoreFlow(api, d)).toBe(true);
    expect(d.questions()).toEqual(["Would you like to restore data from remote storage?\nRestore will delete 4 local notes."]);
    expect(d.log).toContain("info: Data successfully restored.");
  });

  it("local_changed shows its message", async () => {
    d.answers = [true];
    api.commitRestoreError = uiError("local_changed", "Local data changed during the operation. Please try again.");

    expect(await restoreFlow(api, d)).toBe(false);
    expect(d.log).toContain("error: Local data changed during the operation. Please try again.");
  });

  it("no cancels the pending Restore", async () => {
    expect(await restoreFlow(api, d)).toBe(false);
    expect(api.calls).toEqual(["prepareRestore", "cancelPending"]);
  });
});

describe("closeFlow", () => {
  let quits: number;
  const quit = async () => void quits++;

  beforeEach(() => {
    quits = 0;
  });

  it("quits at once without unsaved changes", async () => {
    api.st = { ...api.st, dirty: false };

    await closeFlow(api, d, quit);

    expect(d.log).toEqual([]);
    expect(quits).toBe(1);
  });

  it("quits at once when not connected", async () => {
    api.st = { ...api.st, dirty: true, connected: false };

    await closeFlow(api, d, quit);

    expect(d.log).toEqual([]);
    expect(quits).toBe(1);
  });

  it("asks to save; a successful Backup reports and quits", async () => {
    api.st = { ...api.st, dirty: true };
    d.answers = [true];

    await closeFlow(api, d, quit);

    expect(d.questions()).toEqual(["Would you like to save your data changes to remote database?"]);
    expect(api.calls).toEqual(["prepareBackup", "commitBackup"]);
    expect(d.log.at(-1)).toBe("info: Data successfully synchronized.");
    expect(quits).toBe(1);
  });

  it("Cancel on the save question keeps the window without Backup", async () => {
    api.st = { ...api.st, dirty: true };
    d.answers = ["cancel"];

    await closeFlow(api, d, quit);

    expect(d.questions()).toEqual(["Would you like to save your data changes to remote database?"]);
    expect(api.calls).toEqual([]);
    expect(quits).toBe(0);
  });

  it("no to saving quits without Backup", async () => {
    api.st = { ...api.st, dirty: true };

    await closeFlow(api, d, quit);

    expect(api.calls).toEqual([]);
    expect(quits).toBe(1);
  });

  it("asks about deletions before the Backup", async () => {
    api.st = { ...api.st, dirty: true };
    api.preview = { counts: { added: 0, updated: 0, deleted: 2 }, remoteNewer: false };
    d.answers = [true, false];

    await closeFlow(api, d, quit);

    expect(d.questions()[1]).toContain("Backup will delete 2 notes on the server.");
    expect(api.calls).toEqual(["prepareBackup", "cancelPending"]);
    expect(quits).toBe(0);
  });

  it("a failed Backup shows the error and asks Close anyway?; no keeps the window", async () => {
    api.st = { ...api.st, dirty: true };
    api.commitBackupError = uiError("timeout", "The operation timed out.");
    d.answers = [true, false];

    await closeFlow(api, d, quit);

    expect(d.log).toContain("error: The operation timed out.");
    expect(d.questions().at(-1)).toBe("Close anyway?");
    expect(quits).toBe(0);
  });

  it("a failed Backup on close shows a changed host key with both fingerprints", async () => {
    api.st = { ...api.st, dirty: true };
    api.preview = uiError("host_key_mismatch", "server host key mismatch: expected SHA256:old, got SHA256:new", {
      expected: "SHA256:old",
      got: "SHA256:new",
    });
    d.answers = [true, false];

    await closeFlow(api, d, quit);

    expect(d.log[1]).toContain("The server host key does not match the trusted one.");
    expect(d.log[1]).toContain("Expected: SHA256:old");
  });

  it("Close anyway? yes quits", async () => {
    api.st = { ...api.st, dirty: true };
    api.preview = uiError("ssh", "ssh: connection refused");
    d.answers = [true, true];

    await closeFlow(api, d, quit);

    expect(d.log).toContain("error: ssh: connection refused");
    expect(quits).toBe(1);
  });
});
