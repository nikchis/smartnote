import { afterEach, describe, expect, it } from "vitest";
import { i18n } from "./i18n.svelte";
import { progressView, type Progress, type ProgressView } from "./progress";

// The language switches in place of Go.
i18n.use({ Language: async () => "en", SetLanguage: async () => {} });

const yandex = (direction: string, done: number, total: number): Progress => ({ storage: "yandex_disk", direction, unit: "bytes", done, total });
const google = (direction: string, done: number, total: number): Progress => ({ storage: "google_drive", direction, unit: "bytes", done, total });
const server = (direction: string, done: number, total: number): Progress => ({ storage: "ssh_postgres", direction, unit: "notes", done, total });
const slow = "This can take several minutes on a slow connection. Do not close Smartnote.";

describe("progressView", () => {
  it.each([
    { name: "no progress", p: undefined },
    { name: "the end of the operation", p: { storage: "", direction: "", unit: "", done: 0, total: 0 } },
  ])("is undefined for $name", ({ p }) => {
    expect(progressView(p)).toBeUndefined();
  });

  it.each([
    {
      name: "upload to Yandex Disk",
      p: yandex("upload", 2_400_000, 5_000_000),
      want: { title: "Uploading to Yandex Disk…", detail: "2.4 of 5.0 MB · 48%", warning: slow, value: 0.48 },
    },
    {
      // Prototype П7 (spec §13): Google Drive is not noticeably faster, so it warns as well.
      name: "upload to Google Drive",
      p: google("upload", 2_400_000, 5_000_000),
      want: { title: "Uploading to Google Drive…", detail: "2.4 of 5.0 MB · 48%", warning: slow, value: 0.48 },
    },
    {
      name: "download from Google Drive",
      p: google("download", 0, 1_250_000),
      want: { title: "Synchronizing with Google Drive…", detail: "0.0 of 1.3 MB · 0%", warning: slow, value: 0 },
    },
    {
      name: "download from Yandex Disk",
      p: yandex("download", 0, 1_250_000),
      want: { title: "Synchronizing with Yandex Disk…", detail: "0.0 of 1.3 MB · 0%", warning: slow, value: 0 },
    },
    {
      name: "the last byte sent",
      p: yandex("upload", 5_000_000, 5_000_000),
      want: { title: "Uploading to Yandex Disk…", detail: "Finishing…", warning: slow, value: 1 },
    },
    {
      name: "notes from the server",
      p: server("download", 12, 340),
      want: { title: "Synchronizing with the server…", detail: "12 of 340 notes · 3%", warning: "", value: 12 / 340 },
    },
    {
      name: "notes to the server",
      p: server("upload", 339, 340),
      want: { title: "Uploading to the server…", detail: "339 of 340 notes · 99%", warning: "", value: 339 / 340 },
    },
    {
      name: "an empty snapshot",
      p: server("download", 0, 0),
      want: { title: "Synchronizing with the server…", detail: "Finishing…", warning: "", value: 1 },
    },
  ])("$name", ({ p, want }) => {
    expect(progressView(p)).toEqual(want);
  });

  it.each([
    { name: "Yandex Disk restore download", p: yandex("download", 0, 1_250_000), activity: "restore" as const, title: "Downloading from Yandex Disk…" },
    { name: "server restore download", p: server("download", 12, 340), activity: "restore" as const, title: "Downloading from the server…" },
    { name: "Yandex Disk other download", p: yandex("download", 0, 1_250_000), activity: "sync" as const, title: "Synchronizing with Yandex Disk…" },
    { name: "server other download", p: server("download", 12, 340), activity: "sync" as const, title: "Synchronizing with the server…" },
    { name: "Yandex Disk upload during restore", p: yandex("upload", 1, 2), activity: "restore" as const, title: "Uploading to Yandex Disk…" },
    { name: "server upload during restore", p: server("upload", 1, 2), activity: "restore" as const, title: "Uploading to the server…" },
    { name: "Yandex Disk read of a Backup", p: yandex("download", 0, 1_250_000), activity: "backup" as const, title: "Synchronizing with Yandex Disk…" },
    { name: "server read of a Backup", p: server("download", 12, 340), activity: "backup" as const, title: "Synchronizing with the server…" },
    { name: "Yandex Disk upload of a Backup", p: yandex("upload", 1, 2), activity: "backup" as const, title: "Uploading to Yandex Disk…" },
  ])("titles $name", ({ p, activity, title }) => {
    expect(progressView(p, activity)?.title).toBe(title);
  });

  it.each([
    {
      name: "a transfer in the middle of three",
      p: yandex("download", 650_000, 1_300_000),
      step: { index: 1, count: 3 },
      want: { title: "Synchronizing with Yandex Disk…", detail: "Step 2 of 3 · 0.7 of 1.3 MB · 50%", warning: slow, value: 1.5 / 3 },
    },
    {
      name: "the last of three transfers",
      p: yandex("upload", 2_400_000, 5_000_000),
      step: { index: 2, count: 3 },
      want: { title: "Uploading to Yandex Disk…", detail: "Step 3 of 3 · 2.4 of 5.0 MB · 82%", warning: slow, value: 2.48 / 3 },
    },
    {
      name: "the first of three transfers done",
      p: yandex("download", 1_300_000, 1_300_000),
      step: { index: 0, count: 3 },
      want: { title: "Synchronizing with Yandex Disk…", detail: "Step 1 of 3 · 1.3 of 1.3 MB · 33%", warning: slow, value: 1 / 3 },
    },
    {
      name: "the last of three transfers done",
      p: yandex("upload", 5_000_000, 5_000_000),
      step: { index: 2, count: 3 },
      want: { title: "Uploading to Yandex Disk…", detail: "Step 3 of 3 · Finishing…", warning: slow, value: 1 },
    },
    {
      name: "an empty snapshot as the first of two",
      p: server("download", 0, 0),
      step: { index: 0, count: 2 },
      want: { title: "Synchronizing with the server…", detail: "Step 1 of 2 · 0 of 0 notes · 50%", warning: "", value: 0.5 },
    },
    {
      name: "a transfer beyond the planned ones",
      p: server("upload", 1, 2),
      step: { index: 4, count: 2 },
      want: { title: "Uploading to the server…", detail: "Step 2 of 2 · 1 of 2 notes · 75%", warning: "", value: 0.75 },
    },
  ])("fills its share of the bar: $name", ({ p, step, want }) => {
    const got = progressView(p, "sync", step) as ProgressView;

    expect({ ...got, value: undefined }).toEqual({ ...want, value: undefined });
    expect(got.value).toBeCloseTo(want.value, 9);
  });
});

describe("progressView in Russian", () => {
  afterEach(async () => {
    await i18n.set("en");
  });

  it("follows the language of the interface, with its decimal comma and plural forms", async () => {
    await i18n.set("ru");
    expect(progressView(yandex("upload", 2_400_000, 5_000_000))).toEqual({
      title: "Создание резервной копии на Yandex Disk…",
      detail: "2,4 из 5,0 МБ · 48%",
      warning: "При медленном соединении это может занять несколько минут. Не закрывайте Smartnote.",
      value: 0.48,
    });
    expect(progressView(server("download", 1, 21), "restore", { index: 1, count: 2 })?.detail).toBe("Шаг 2 из 2 · 1 из 21 заметки · 52%");
    expect(progressView(server("download", 12, 340))?.title).toBe("Синхронизация с сервером…");
    expect(progressView(server("upload", 340, 340))?.detail).toBe("Завершение…");
  });

  // Spec §16.4, decision of the customer: the progress names the Backup and the Restore by their nouns.
  it.each([
    { name: "Yandex Disk upload", p: yandex("upload", 1, 2), activity: "sync" as const, title: "Создание резервной копии на Yandex Disk…" },
    { name: "server upload", p: server("upload", 1, 2), activity: "backup" as const, title: "Создание резервной копии на сервере…" },
    { name: "Google Drive upload", p: google("upload", 1, 2), activity: "backup" as const, title: "Создание резервной копии на Google Drive…" },
    { name: "Google Drive Restore download", p: google("download", 0, 2), activity: "restore" as const, title: "Восстановление копии с Google Drive…" },
    { name: "Yandex Disk read of a Backup", p: yandex("download", 0, 2), activity: "backup" as const, title: "Создание резервной копии на Yandex Disk…" },
    { name: "server read of a Backup", p: server("download", 1, 2), activity: "backup" as const, title: "Создание резервной копии на сервере…" },
    { name: "Yandex Disk Restore download", p: yandex("download", 0, 2), activity: "restore" as const, title: "Восстановление копии с Yandex Disk…" },
    { name: "server Restore download", p: server("download", 1, 2), activity: "restore" as const, title: "Восстановление копии с сервера…" },
    { name: "Yandex Disk read of Connect", p: yandex("download", 0, 2), activity: "sync" as const, title: "Синхронизация с Yandex Disk…" },
    { name: "server read of Connect", p: server("download", 1, 2), activity: "sync" as const, title: "Синхронизация с сервером…" },
  ])("titles the $name with the nouns of Backup and Restore", async ({ p, activity, title }) => {
    await i18n.set("ru");
    expect(progressView(p, activity)?.title).toBe(title);
  });
});
