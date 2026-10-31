import { describe, expect, it } from "vitest";
import { passwordPairOK } from "./password";

describe("passwordPairOK", () => {
  it.each([
    { name: "too short", pw: "12345", confirm: "12345", ok: false },
    { name: "shortest", pw: "123456", confirm: "123456", ok: true },
    { name: "longest", pw: "x".repeat(64), confirm: "x".repeat(64), ok: true },
    { name: "too long", pw: "x".repeat(65), confirm: "x".repeat(65), ok: false },
    { name: "mismatch", pw: "123456", confirm: "123457", ok: false },
    { name: "same after NFC", pw: "caféxx", confirm: "caféxx", ok: true },
    { name: "spaces count", pw: "  12  ", confirm: "  12  ", ok: true },
  ])("$name", ({ pw, confirm, ok }) => {
    expect(passwordPairOK(pw, confirm)).toBe(ok);
  });
});
