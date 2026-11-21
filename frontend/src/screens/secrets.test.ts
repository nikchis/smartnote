import { describe, expect, it } from "vitest";
import { emptySettings, toForm } from "../lib/settings";

// Secret lengths are checked after NFC (spec §5.4); maxlength would cut the
// typed text before that, so a valid password in NFD could not be entered.
const screens = import.meta.glob("./*.svelte", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

describe("password inputs", () => {
  const inputs = Object.entries(screens).flatMap(([file, src]) =>
    [...src.matchAll(/<input\b[^>]*type="password"[^>]*>/g)].map((m) => ({ file, tag: m[0] })),
  );

  it("exist in the screens", () => {
    expect(inputs.length).toBeGreaterThan(0);
  });

  it.each(inputs)("$file has no maxlength", ({ tag }) => {
    expect(tag).not.toMatch(/maxlength/);
  });
});

// The form holds the settings only; tokens and the sign-in state stay in Go,
// which reports the account of the draft through Account (spec §8, §1.2).
describe("settings form data", () => {
  it.each([
    { name: "the settings", keys: Object.keys(emptySettings) },
    { name: "the form", keys: Object.keys(toForm(emptySettings)) },
  ])("$name carry no token and no sign-in state", ({ keys }) => {
    expect(keys.length).toBeGreaterThan(0);
    for (const k of keys) expect(k).not.toMatch(/token|refresh|access|account|signedin/i);
  });
});
