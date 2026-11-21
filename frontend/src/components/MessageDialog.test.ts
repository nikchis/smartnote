// @vitest-environment happy-dom
import { flushSync, mount, unmount } from "svelte";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { currentDialog, dialogs, dismiss } from "../lib/dialogs.svelte";
import { msg } from "../lib/i18n.svelte";
import MessageDialog from "./MessageDialog.svelte";

let target: HTMLElement;
let view: ReturnType<typeof MessageDialog>;

beforeEach(() => {
  target = document.createElement("div");
  document.body.append(target);
  view = mount(MessageDialog, { target });
});

afterEach(() => {
  while (currentDialog()) dismiss();
  flushSync();
  unmount(view);
  target.remove();
});

const buttons = () => [...target.querySelectorAll("button")].map((b) => b.textContent);
const focused = () => (document.activeElement as HTMLElement | null)?.textContent;

const show = {
  info: () => dialogs.info(msg("sync.saved")),
  warning: () => dialogs.warning(msg("sync.olderWarning")),
  error: () => dialogs.error({ raw: "Failed." }),
  ask: () => dialogs.ask(msg("editor.removeConfirm")),
  choose: () => dialogs.choose(msg("sync.closeQuestion")),
} as const;

describe("MessageDialog", () => {
  it.each([
    ["info", "success", null],
    ["warning", "warning", "Warning"],
    ["error", "error", "Error"],
    ["ask", "question", "Question"],
    ["choose", "question", "Question"],
  ] as const)("shows a %s with the %s mark and title %s", (kind, mark, title) => {
    void show[kind]();
    flushSync();

    expect(target.querySelector("svg.mark")?.getAttribute("data-mark")).toBe(mark);
    expect(target.querySelector("h2")?.textContent ?? null).toBe(title);
    expect(target.querySelector("img")).toBeNull();
  });

  it("names a dialog without a title by its message", () => {
    void dialogs.info(msg("sync.saved"));
    flushSync();

    const id = target.querySelector('[role="alertdialog"]')?.getAttribute("aria-labelledby");
    expect(id && document.getElementById(id)?.textContent).toBe("Data successfully saved.");
  });

  it("focuses Yes of a question", () => {
    void dialogs.ask(msg("editor.removeConfirm"));
    flushSync();

    expect(buttons()).toEqual(["Yes", "No"]);
    expect(focused()).toBe("Yes");
  });

  it("focuses Cancel of the Discard question, so a repeated Enter keeps the draft", () => {
    void dialogs.ask(msg("editor.discard"), { yes: "editor.discardYes", no: "common.cancel", primary: "no" });
    flushSync();

    expect(buttons()).toEqual(["Discard", "Cancel"]);
    expect(focused()).toBe("Cancel");
  });
});
