import { describe, expect, it } from "vitest";
import { parseError } from "./errors";
// The exact bytes Go produces (TestUIErrorGolden in internal/adapter/wailsui).
import golden from "./testdata/uierror-host-key-unknown.json?raw";

describe("parseError", () => {
  it("reads the UIError JSON of a rejected binding call", () => {
    const e = parseError(new Error(golden));
    expect(e).toEqual({ code: "host_key_unknown", message: "m", fingerprint: "SHA256:x", address: "example.org:22" });
  });

  it("treats a plain string as internal", () => {
    expect(parseError("boom")).toEqual({ code: "internal", message: "boom" });
  });

  it("treats an Error without JSON as internal", () => {
    expect(parseError(new Error("network down"))).toEqual({ code: "internal", message: "network down" });
  });

  it("treats JSON without a code as internal", () => {
    expect(parseError(new Error('{"message":"x"}'))).toEqual({ code: "internal", message: '{"message":"x"}' });
  });

  it("drops fields of the wrong type", () => {
    const e = parseError(new Error('{"code":"validation","message":"m","field":7}'));
    expect(e).toEqual({ code: "validation", message: "m" });
  });
});
