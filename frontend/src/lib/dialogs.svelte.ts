// Message dialogs of v1 (UDlgMsg): information, error, a yes/no question,
// and a choice with Cancel, plus a warning. Dialogs queue up and
// MessageDialog shows the first one. A dialog keeps its message and buttons
// as keys, not as text: it shows them in the language of the moment, also
// after a switch while it is open or waiting (spec §16.4).
import type { Key, Msg } from "./i18n.svelte";

export type DialogKind = "info" | "warning" | "error" | "ask" | "choose";

/** Answer is the button pressed; OK of info, warning and error is "yes". */
export type Answer = "yes" | "no" | "cancel";

/**
 * Labels name the Yes and No buttons of a question, as Discard and Cancel;
 * primary is the button with the focus, Yes unless it is "no".
 */
export type Labels = { yes: Key; no: Key; primary?: "yes" | "no" };

type Pending = { kind: DialogKind; message: Msg; labels?: Labels; resolve: (a: Answer) => void };

const state = $state<{ queue: Pending[] }>({ queue: [] });

function show(kind: DialogKind, message: Msg, labels?: Labels): Promise<Answer> {
  return new Promise((resolve) => state.queue.push({ kind, message, labels, resolve }));
}

/** Dialogs is what screens and flows need; tests pass a fake. */
export type Dialogs = {
  /** info reports an action that is done, as "Data successfully saved.". */
  info(msg: Msg): Promise<void>;
  /** warning reports a state the user should act on; nothing failed. */
  warning(msg: Msg): Promise<void>;
  error(msg: Msg): Promise<void>;
  /** ask asks with Yes and No, or with labels in their place; Escape gives No. */
  ask(msg: Msg, labels?: Labels): Promise<boolean>;
  /** choose asks with Yes, No and Cancel; Cancel and Escape give "cancel". */
  choose(msg: Msg): Promise<Answer>;
};

export const dialogs: Dialogs = {
  async info(msg) {
    await show("info", msg);
  },
  async warning(msg) {
    await show("warning", msg);
  },
  async error(msg) {
    await show("error", msg);
  },
  async ask(msg, labels) {
    return (await show("ask", msg, labels)) === "yes";
  },
  choose(msg) {
    return show("choose", msg);
  },
};

/** currentDialog is the dialog on screen, if any. */
export function currentDialog(): { kind: DialogKind; message: Msg; labels?: Labels } | undefined {
  const d = state.queue[0];
  return d && { kind: d.kind, message: d.message, labels: d.labels };
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
