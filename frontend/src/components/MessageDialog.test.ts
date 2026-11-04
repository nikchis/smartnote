// @vitest-environment happy-dom
import { flushSync, mount, unmount } from "svelte";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { currentDialog, dialogs, dismiss } from "../lib/dialogs.svelte";
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

describe("MessageDialog", () => {
  it("focuses Yes of a question", () => {
    void dialogs.ask("Are you sure you want to remove this note?");
    flushSync();

    expect(buttons()).toEqual(["Yes", "No"]);
    expect(focused()).toBe("Yes");
  });

  it("focuses Cancel of the Discard question, so a repeated Enter keeps the draft", () => {
    void dialogs.ask("Discard unsaved changes?", { yes: "Discard", no: "Cancel", primary: "no" });
    flushSync();

    expect(buttons()).toEqual(["Discard", "Cancel"]);
    expect(focused()).toBe("Cancel");
  });
});
