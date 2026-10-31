import { afterEach, describe, expect, it } from "vitest";
import { answer, currentDialog, dialogs, dismiss } from "./dialogs.svelte";

afterEach(() => {
  while (currentDialog()) dismiss();
});

describe("dialogs", () => {
  it("choose shows Yes, No and Cancel and resolves to the button pressed", async () => {
    for (const pressed of ["yes", "no", "cancel"] as const) {
      const result = dialogs.choose("Save?");
      expect(currentDialog()).toEqual({ kind: "choose", message: "Save?" });
      answer(pressed);
      await expect(result).resolves.toBe(pressed);
    }
  });

  it("Escape cancels a choice", async () => {
    const result = dialogs.choose("Save?");
    dismiss();
    await expect(result).resolves.toBe("cancel");
  });

  it("Escape answers No to a yes/no question", async () => {
    const result = dialogs.ask("Delete?");
    dismiss();
    await expect(result).resolves.toBe(false);
  });

  it("ask is true only for Yes", async () => {
    const yes = dialogs.ask("Delete?");
    answer("yes");
    await expect(yes).resolves.toBe(true);

    const no = dialogs.ask("Delete?");
    answer("no");
    await expect(no).resolves.toBe(false);
  });
});
