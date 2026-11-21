import { describe, expect, it } from "vitest";

const sources = import.meta.glob(["../**/*.svelte", "../**/*.ts", "../**/*.html"], {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

describe("frontend sources", () => {
  it("no en or em dash in frontend sources", () => {
    const files = Object.entries(sources).filter(([path]) => !path.endsWith(".test.ts"));
    expect(files.length).toBeGreaterThan(0);
    const offenders = files.filter(([, text]) => /[\u2013\u2014]/.test(text)).map(([path]) => path);
    expect(offenders).toEqual([]);
  });
});
