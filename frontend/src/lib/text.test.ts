import { describe, expect, it } from "vitest";
import vectors from "../../../testdata/lengths.json";
import { LIMITS, normalizeText, secretLen, utf16Len } from "./text";

describe("utf16Len", () => {
  it.each(vectors)("$name", ({ text, utf16 }) => {
    expect(utf16Len(text)).toBe(utf16);
  });
});

describe("normalizeText", () => {
  it("turns CRLF into LF and keeps a lone CR", () => {
    expect(normalizeText("a\r\nb\rc")).toBe("a\nb\rc");
  });
});

describe("secretLen", () => {
  it("counts after NFC, like domain.NormalizeSecret", () => {
    expect(secretLen("é")).toBe(1);
    expect(secretLen(" pw ")).toBe(4);
  });
});

describe("LIMITS", () => {
  it("matches the domain limits", () => {
    expect(LIMITS).toEqual({ name: 64, info: 8192, passwordMin: 6, passwordMax: 64, phraseMin: 16 });
  });
});
