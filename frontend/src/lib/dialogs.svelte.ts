// Message dialogs of v1 (UDlgMsg): information, error, a yes/no question,
// and a choice with Cancel. Dialogs queue up and MessageDialog shows the
// first one.

export type DialogKind = "info" | "error" | "ask" | "choose";

/** Answer is the button pressed; OK of info and error is "yes". */
export type Answer = "yes" | "no" | "cancel";

type Pending = { kind: DialogKind; message: string; resolve: (a: Answer) => void };

const state = $state<{ queue: Pending[] }>({ queue: [] });

function show(kind: DialogKind, message: string): Promise<Answer> {
  return new Promise((resolve) => state.queue.push({ kind, message, resolve }));
}

/** Dialogs is what screens and flows need; tests pass a fake. */
export type Dialogs = {
  info(msg: string): Promise<void>;
  error(msg: string): Promise<void>;
  ask(msg: string): Promise<boolean>;
  /** choose asks with Yes, No and Cancel; Cancel and Escape give "cancel". */
  choose(msg: string): Promise<Answer>;
};

export const dialogs: Dialogs = {
  async info(msg) {
    await show("info", msg);
  },
  async error(msg) {
    await show("error", msg);
  },
  async ask(msg) {
    return (await show("ask", msg)) === "yes";
  },
  choose(msg) {
    return show("choose", msg);
  },
};

/** currentDialog is the dialog on screen, if any. */
export function currentDialog(): { kind: DialogKind; message: string } | undefined {
  const d = state.queue[0];
  return d && { kind: d.kind, message: d.message };
}

/** answer closes the dialog on screen with the button pressed. */
export function answer(a: Answer): void {
  state.queue.shift()?.resolve(a);
}

/** dismiss is Escape: Cancel for a choice, No for any other dialog. */
export function dismiss(): void {
  const d = state.queue[0];
  if (d) answer(d.kind === "choose" ? "cancel" : "no");
}
