import { describe, expect, it } from "vitest";
import { strength } from "./strength";

describe("strength", () => {
  it("rates an empty and a common phrase weak, a long random one strong", () => {
    expect(strength("")).toBe(0);
    expect(strength("password12345678")).toBeLessThanOrEqual(1);
    expect(strength("tq8#Vb2!kL9zR4@wN7mx")).toBe(4);
  });
});
