import { describe, expect, it } from "vitest";
import { progressView, type Progress } from "./progress";

const yandex = (direction: string, done: number, total: number): Progress => ({ storage: "yandex_disk", direction, unit: "bytes", done, total });
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
      want: { title: "Uploading to Yandex Disk…", detail: "2.4 of 5.0 MB · 48%", warning: slow, done: 2_400_000, total: 5_000_000 },
    },
    {
      name: "download from Yandex Disk",
      p: yandex("download", 0, 1_250_000),
      want: { title: "Synchronizing with Yandex Disk…", detail: "0.0 of 1.3 MB · 0%", warning: slow, done: 0, total: 1_250_000 },
    },
    {
      name: "the last byte sent",
      p: yandex("upload", 5_000_000, 5_000_000),
      want: { title: "Uploading to Yandex Disk…", detail: "Finishing…", warning: slow, done: 5_000_000, total: 5_000_000 },
    },
    {
      name: "notes from the server",
      p: server("download", 12, 340),
      want: { title: "Synchronizing with the server…", detail: "12 of 340 notes · 3%", warning: "", done: 12, total: 340 },
    },
    {
      name: "notes to the server",
      p: server("upload", 339, 340),
      want: { title: "Uploading to the server…", detail: "339 of 340 notes · 99%", warning: "", done: 339, total: 340 },
    },
    {
      name: "an empty snapshot",
      p: server("download", 0, 0),
      want: { title: "Synchronizing with the server…", detail: "Finishing…", warning: "", done: 0, total: 0 },
    },
  ])("$name", ({ p, want }) => {
    expect(progressView(p)).toEqual(want);
  });

  it.each([
    { name: "Yandex Disk restore download", p: yandex("download", 0, 1_250_000), restoring: true, title: "Downloading from Yandex Disk…" },
    { name: "server restore download", p: server("download", 12, 340), restoring: true, title: "Downloading from the server…" },
    { name: "Yandex Disk other download", p: yandex("download", 0, 1_250_000), restoring: false, title: "Synchronizing with Yandex Disk…" },
    { name: "server other download", p: server("download", 12, 340), restoring: false, title: "Synchronizing with the server…" },
    { name: "Yandex Disk upload during restore", p: yandex("upload", 1, 2), restoring: true, title: "Uploading to Yandex Disk…" },
    { name: "server upload during restore", p: server("upload", 1, 2), restoring: true, title: "Uploading to the server…" },
  ])("titles $name", ({ p, restoring, title }) => {
    expect(progressView(p, restoring)?.title).toBe(title);
  });
});
