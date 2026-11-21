import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { en } from "./i18n/en";
import { ru } from "./i18n/ru";
import { I18n, i18n, msg } from "./i18n.svelte";

// Go keeps the language (PrefsService); the test answers for it.
const prefs = { Language: vi.fn(), SetLanguage: vi.fn() };
i18n.use(prefs);

beforeEach(() => {
  prefs.Language.mockReset();
  prefs.SetLanguage.mockReset();
  prefs.SetLanguage.mockResolvedValue(undefined);
});

afterEach(async () => {
  prefs.SetLanguage.mockResolvedValue(undefined);
  await i18n.set("en");
});

/** placeholders are the names of {name} and {name|form|...} in text, each once, sorted. */
const placeholders = (text: string) => [...new Set([...text.matchAll(/\{(\w+)[^}]*\}/g)].map((m) => m[1]))].sort();
/** forms are the counts of plural forms of the placeholders of text. */
const forms = (text: string) => [...text.matchAll(/\{\w+((?:\|[^|}]*)+)\}/g)].map((m) => m[1].split("|").length - 1);

describe("catalogs", () => {
  it("catalogs have the same keys and no empty values", () => {
    expect(Object.keys(ru).sort()).toEqual(Object.keys(en).sort());
    for (const catalog of [en, ru]) {
      const empty = Object.entries(catalog).filter(([, text]) => text.trim() === "");
      expect(empty).toEqual([]);
    }
  });

  it("no dashes in catalogs", () => {
    const offenders = [...Object.entries(en), ...Object.entries(ru)].filter(([, text]) => /[–—]/.test(text));
    expect(offenders).toEqual([]);
  });

  it("translations take the same parameters", () => {
    const differ = (Object.keys(en) as (keyof typeof en)[]).filter((k) => placeholders(en[k]).join() !== placeholders(ru[k]).join());
    expect(differ).toEqual([]);
  });

  it("plural forms follow the language: two in English, three in Russian", () => {
    expect(Object.values(en).flatMap(forms).filter((n) => n !== 2)).toEqual([]);
    expect(Object.values(ru).flatMap(forms).filter((n) => n !== 3)).toEqual([]);
  });
});

describe("I18n", () => {
  it("starts from saved or system language", async () => {
    prefs.Language.mockResolvedValue("ru");
    await i18n.start("en-US");
    expect(prefs.Language).toHaveBeenCalledWith("en-US");
    expect(i18n.lang).toBe("ru");
    expect(i18n.t("main.settings")).toBe("Настройки");
  });

  it.each([
    ["ru-RU", "ru"],
    ["ru", "ru"],
    ["de-DE", "en"],
  ] as const)("starts from the system language %s when Go cannot answer", async (system, lang) => {
    prefs.Language.mockRejectedValue(new Error("no Go"));
    await i18n.start(system);
    expect(i18n.lang).toBe(lang);
  });

  it("without the store of the choice starts from the system language and cannot switch", async () => {
    const alone = new I18n();
    await alone.start("ru-RU");
    expect(alone.lang).toBe("ru");
    await expect(alone.set("en")).rejects.toThrow();
    expect(alone.lang).toBe("ru");
  });

  it("switches after Go saved the language", async () => {
    await i18n.set("ru");
    expect(prefs.SetLanguage).toHaveBeenCalledWith("ru");
    expect(i18n.lang).toBe("ru");
  });

  it("keeps the language when Go fails to save it", async () => {
    prefs.SetLanguage.mockRejectedValue(new Error('{"code":"internal","message":"disk full"}'));
    await expect(i18n.set("ru")).rejects.toThrow("disk full");
    expect(i18n.lang).toBe("en");
  });

  it("substitutes parameters and picks the plural form of the language", async () => {
    expect(i18n.t("sync.restoreDeletes", { n: 2 })).toBe("Restore will delete 2 local notes.");
    await i18n.set("ru");
    expect(i18n.t("sync.restoreDeletes", { n: 1 })).toBe("Восстановление копии удалит 1 локальную заметку.");
    expect(i18n.t("sync.restoreDeletes", { n: 3 })).toBe("Восстановление копии удалит 3 локальные заметки.");
    expect(i18n.t("sync.restoreDeletes", { n: 11 })).toBe("Восстановление копии удалит 11 локальных заметок.");
    expect(i18n.t("sync.restoreDeletes", { n: 21 })).toBe("Восстановление копии удалит 21 локальную заметку.");
  });

  it("does not read parameters inside parameter values", () => {
    expect(i18n.t("account.signedInAs", { account: "{account}" })).toBe("Signed in as {account}");
  });

  it("text shows each kind of message in the current language", async () => {
    const go = { messages: { en: "Remote storage is not configured.", ru: "Удалённое хранилище не настроено." } };
    const lines = { lines: [msg("sync.backupQuestion"), msg("sync.backupDeletes", { n: 5 })] };
    expect(i18n.text(msg("common.yes"))).toBe("Yes");
    expect(i18n.text(go)).toBe("Remote storage is not configured.");
    expect(i18n.text({ raw: "ssh: connection refused" })).toBe("ssh: connection refused");
    expect(i18n.text(lines)).toBe("Would you like to save data on remote storage?\nBackup will delete 5 notes on the server.");

    await i18n.set("ru");
    expect(i18n.text(msg("common.yes"))).toBe("Да");
    expect(i18n.text(go)).toBe("Удалённое хранилище не настроено.");
    expect(i18n.text({ raw: "ssh: connection refused" })).toBe("ssh: connection refused");
    expect(i18n.text(lines)).toBe("Сохранить данные в удалённом хранилище?\nСоздание резервной копии удалит 5 заметок на сервере.");
  });
});
