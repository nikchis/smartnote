import { describe, expect, it } from "vitest";
import { progressView, type Progress, type ProgressView } from "./progress";

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
      want: { title: "Uploading to Yandex Disk…", detail: "2.4 of 5.0 MB · 48%", warning: slow, value: 0.48 },
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
    { name: "Yandex Disk restore download", p: yandex("download", 0, 1_250_000), restoring: true, title: "Downloading from Yandex Disk…" },
    { name: "server restore download", p: server("download", 12, 340), restoring: true, title: "Downloading from the server…" },
    { name: "Yandex Disk other download", p: yandex("download", 0, 1_250_000), restoring: false, title: "Synchronizing with Yandex Disk…" },
    { name: "server other download", p: server("download", 12, 340), restoring: false, title: "Synchronizing with the server…" },
    { name: "Yandex Disk upload during restore", p: yandex("upload", 1, 2), restoring: true, title: "Uploading to Yandex Disk…" },
    { name: "server upload during restore", p: server("upload", 1, 2), restoring: true, title: "Uploading to the server…" },
  ])("titles $name", ({ p, restoring, title }) => {
    expect(progressView(p, restoring)?.title).toBe(title);
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
    const got = progressView(p, false, step) as ProgressView;

    expect({ ...got, value: undefined }).toEqual({ ...want, value: undefined });
    expect(got.value).toBeCloseTo(want.value, 9);
  });
});
