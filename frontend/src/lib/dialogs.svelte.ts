// Message dialogs of v1 (UDlgMsg): information, error, and a yes/no question.
// Dialogs queue up and MessageDialog shows the first one.

export type DialogKind = "info" | "error" | "ask";

type Pending = { kind: DialogKind; message: string; resolve: (yes: boolean) => void };

const state = $state<{ queue: Pending[] }>({ queue: [] });

function show(kind: DialogKind, message: string): Promise<boolean> {
  return new Promise((resolve) => state.queue.push({ kind, message, resolve }));
}

/** Dialogs is what screens and flows need; tests pass a fake. */
export type Dialogs = {
  info(msg: string): Promise<void>;
  error(msg: string): Promise<void>;
  ask(msg: string): Promise<boolean>;
};

export const dialogs: Dialogs = {
  async info(msg) {
    await show("info", msg);
  },
  async error(msg) {
    await show("error", msg);
  },
  ask(msg) {
    return show("ask", msg);
  },
};

/** currentDialog is the dialog on screen, if any. */
export function currentDialog(): { kind: DialogKind; message: string } | undefined {
  return state.queue[0];
}

/** answer closes the dialog on screen; yes is false for "No". */
export function answer(yes: boolean): void {
  state.queue.shift()?.resolve(yes);
}
