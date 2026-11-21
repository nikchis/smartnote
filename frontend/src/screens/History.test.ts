// @vitest-environment happy-dom
import { flushSync, mount, unmount } from "svelte";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { i18n } from "../lib/i18n.svelte";
import History from "./History.svelte";

i18n.use({ Language: async () => "en", SetLanguage: async () => {} });

let target: HTMLElement;
let view: ReturnType<typeof mount>;

beforeEach(() => {
  target = document.createElement("div");
  document.body.append(target);
  view = mount(History, { target, props: { versions: [{ id: "v1", created: 1_700_000_000_000, info: "old" }], onUse: () => {}, onClose: () => {} } });
  flushSync();
});

afterEach(async () => {
  unmount(view);
  target.remove();
  await i18n.set("en");
});

describe("History", () => {
  it("shows the dates in the language of the interface", async () => {
    const date = () => target.querySelector(".list button")?.textContent?.trim();
    expect(date()).toBe(new Date(1_700_000_000_000).toLocaleString("en"));

    await i18n.set("ru");
    flushSync();

    expect(date()).toBe(new Date(1_700_000_000_000).toLocaleString("ru"));
    expect(target.querySelector("h2")?.textContent).toBe("История");
  });
});
