import { afterEach, describe, expect, it } from "vitest";
import { answer, currentDialog, dialogs, dismiss } from "./dialogs.svelte";

afterEach(() => {
  while (currentDialog()) dismiss();
});

describe("dialogs", () => {
  it("warning shows a warning with OK and resolves on OK", async () => {
    let done = false;
    const result = dialogs.warning("The copy is older.").then(() => (done = true));
    expect(currentDialog()).toEqual({ kind: "warning", message: "The copy is older." });
    answer("yes");
    await result;
    expect(done).toBe(true);
  });

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

  it("ask with labels shows them and answers like Yes and No", async () => {
    const labels = { yes: "Discard", no: "Cancel", primary: "no" } as const;
    const discard = dialogs.ask("Discard unsaved changes in the editor?", labels);
    expect(currentDialog()).toEqual({ kind: "ask", message: "Discard unsaved changes in the editor?", labels });
    answer("yes");
    await expect(discard).resolves.toBe(true);

    const escape = dialogs.ask("Discard unsaved changes in the editor?", labels);
    dismiss();
    await expect(escape).resolves.toBe(false);
  });
});
