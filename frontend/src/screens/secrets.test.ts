import { describe, expect, it } from "vitest";

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
