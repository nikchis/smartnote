// @vitest-environment happy-dom
import { flushSync, mount, unmount } from "svelte";
import { afterEach, describe, expect, it } from "vitest";
import { progressView, type Progress } from "../lib/progress";
import Overlay from "./Overlay.svelte";

let target: HTMLElement | undefined;
let view: ReturnType<typeof Overlay> | undefined;

function show(text: string, p?: Progress): HTMLElement {
  target = document.createElement("div");
  document.body.append(target);
  view = mount(Overlay, { target, props: { text, progress: progressView(p) } });
  flushSync();
  return target;
}

afterEach(() => {
  if (view) unmount(view);
  target?.remove();
  view = target = undefined;
});

const lines = (el: HTMLElement) => [...el.querySelectorAll(".box p")].map((p) => p.textContent);

describe("Overlay", () => {
  it("shows only the text without progress", () => {
    const el = show("Synchronizing…");

    expect(el.querySelector(".box")?.textContent?.trim()).toBe("Synchronizing…");
    expect(el.querySelector("progress")).toBeNull();
  });

  it("shows the bytes of a Yandex Disk upload with the warning", () => {
    const el = show("Synchronizing…", { storage: "yandex_disk", direction: "upload", unit: "bytes", done: 2_400_000, total: 5_000_000 });

    expect(lines(el)).toEqual([
      "Uploading to Yandex Disk…",
      "2.4 of 5.0 MB · 48%",
      "This can take several minutes on a slow connection. Do not close Smartnote.",
    ]);
    const bar = el.querySelector("progress")!;
    expect(bar.max).toBe(1);
    expect(bar.value).toBeCloseTo(0.48, 9);
    expect(bar.getAttribute("aria-label")).toBe("Uploading to Yandex Disk…");
    expect(bar.getAttribute("aria-valuetext")).toBe("2.4 of 5.0 MB · 48%");
  });

  it("shows Finishing… without numbers after the last byte", () => {
    const el = show("Synchronizing…", { storage: "yandex_disk", direction: "download", unit: "bytes", done: 900, total: 900 });

    expect(lines(el)).toEqual([
      "Synchronizing with Yandex Disk…",
      "Finishing…",
      "This can take several minutes on a slow connection. Do not close Smartnote.",
    ]);
  });

  it("fills the bar beside Finishing… when there is nothing to transfer", () => {
    const el = show("Synchronizing…", { storage: "yandex_disk", direction: "download", unit: "bytes", done: 0, total: 0 });
    const bar = el.querySelector("progress")!;

    expect(lines(el)[1]).toBe("Finishing…");
    expect(bar.max).toBe(1);
    expect(bar.value).toBe(1);
  });

  it("shows the notes of the server without the Yandex Disk warning", () => {
    const el = show("Connecting…", { storage: "ssh_postgres", direction: "download", unit: "notes", done: 12, total: 340 });

    expect(lines(el)).toEqual(["Synchronizing with the server…", "12 of 340 notes · 3%"]);
    expect(el.querySelector("progress")?.value).toBeCloseTo(12 / 340, 9);
  });
});
