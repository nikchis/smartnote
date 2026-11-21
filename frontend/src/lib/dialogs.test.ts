// @vitest-environment happy-dom
import { flushSync, mount, unmount } from "svelte";
import { afterEach, describe, expect, it } from "vitest";
import MessageDialog from "../components/MessageDialog.svelte";
import { answer, currentDialog, dialogs, dismiss } from "./dialogs.svelte";
import { errorMsg } from "./errors";
import { i18n, msg } from "./i18n.svelte";

// The language switches in place of Go.
i18n.use({ Language: async () => "en", SetLanguage: async () => {} });

afterEach(async () => {
  while (currentDialog()) dismiss();
  await i18n.set("en");
});

describe("dialogs", () => {
  it("warning shows a warning with OK and resolves on OK", async () => {
    let done = false;
    const result = dialogs.warning({ raw: "The copy is older." }).then(() => (done = true));
    expect(currentDialog()).toEqual({ kind: "warning", message: { raw: "The copy is older." } });
    answer("yes");
    await result;
    expect(done).toBe(true);
  });

  it("choose shows Yes, No and Cancel and resolves to the button pressed", async () => {
    for (const pressed of ["yes", "no", "cancel"] as const) {
      const result = dialogs.choose(msg("sync.closeQuestion"));
      expect(currentDialog()).toEqual({ kind: "choose", message: msg("sync.closeQuestion") });
      answer(pressed);
      await expect(result).resolves.toBe(pressed);
    }
  });

  it("Escape cancels a choice", async () => {
    const result = dialogs.choose(msg("sync.closeQuestion"));
    dismiss();
    await expect(result).resolves.toBe("cancel");
  });

  it("Escape answers No to a yes/no question", async () => {
    const result = dialogs.ask(msg("editor.removeConfirm"));
    dismiss();
    await expect(result).resolves.toBe(false);
  });

  it("ask is true only for Yes", async () => {
    const yes = dialogs.ask(msg("editor.removeConfirm"));
    answer("yes");
    await expect(yes).resolves.toBe(true);

    const no = dialogs.ask(msg("editor.removeConfirm"));
    answer("no");
    await expect(no).resolves.toBe(false);
  });

  it("ask with labels shows them and answers like Yes and No", async () => {
    const labels = { yes: "editor.discardYes", no: "common.cancel", primary: "no" } as const;
    const discard = dialogs.ask(msg("editor.discard"), labels);
    expect(currentDialog()).toEqual({ kind: "ask", message: msg("editor.discard"), labels });
    answer("yes");
    await expect(discard).resolves.toBe(true);

    const escape = dialogs.ask(msg("editor.discard"), labels);
    dismiss();
    await expect(escape).resolves.toBe(false);
  });
});

describe("dialogs and the language", () => {
  let target: HTMLElement;
  let view: ReturnType<typeof MessageDialog>;

  const render = () => {
    target = document.createElement("div");
    document.body.append(target);
    view = mount(MessageDialog, { target });
    flushSync();
  };
  const shown = () => ({
    title: target.querySelector("h2")?.textContent ?? null,
    message: target.querySelector("#dialog-message")?.textContent,
    buttons: [...target.querySelectorAll("button")].map((b) => b.textContent),
  });

  afterEach(() => {
    while (currentDialog()) dismiss();
    flushSync();
    unmount(view);
    target.remove();
  });

  it("switching language re-renders open dialog text and custom buttons", async () => {
    void dialogs.ask(msg("editor.discard"), { yes: "editor.discardYes", no: "common.cancel", primary: "no" });
    render();
    expect(shown()).toEqual({ title: "Question", message: "Discard unsaved changes in the editor?", buttons: ["Discard", "Cancel"] });

    await i18n.set("ru");
    flushSync();
    expect(shown()).toEqual({ title: "Вопрос", message: "Сбросить несохранённые изменения в редакторе?", buttons: ["Сбросить", "Отмена"] });
  });

  it("queued dialog shows the current language when it opens", async () => {
    void dialogs.info(msg("editor.added"));
    void dialogs.choose(msg("sync.closeQuestion"));
    render();
    expect(shown().message).toBe("Note has been successfully added.");

    await i18n.set("ru");
    answer("yes");
    flushSync();
    expect(shown()).toEqual({
      title: "Вопрос",
      message: "Сохранить изменения данных в удалённом хранилище?",
      buttons: ["Да", "Нет", "Отмена"],
    });
  });

  it("Go error follows language", async () => {
    const messages = { en: "Remote storage is not configured.", ru: "Удалённое хранилище не настроено." };
    const go = new Error(JSON.stringify({ code: "not_connected", message: messages.en, messages }));
    void dialogs.error(errorMsg(go));
    void dialogs.error(errorMsg(new Error("ssh: connection refused")));
    render();
    expect(shown()).toEqual({ title: "Error", message: "Remote storage is not configured.", buttons: ["OK"] });

    await i18n.set("ru");
    flushSync();
    expect(shown()).toEqual({ title: "Ошибка", message: "Удалённое хранилище не настроено.", buttons: ["OK"] });

    // A technical text of a library has no translation and stays as it is.
    answer("yes");
    flushSync();
    expect(shown().message).toBe("ssh: connection refused");
  });
});
