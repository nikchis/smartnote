import { describe, expect, it } from "vitest";
import type { SettingsDTO } from "./api";
import { emptySettings, fromForm, portValue, toForm } from "./settings";

describe("portValue", () => {
  it.each([
    { input: "", want: 0 },
    { input: "  ", want: 0 },
    { input: "22", want: 22 },
    { input: " 5432 ", want: 5432 },
    { input: "70000", want: 70000 },
    { input: "abc", want: -1 },
    { input: "22.5", want: -1 },
    { input: "-5", want: -5 },
    { input: "1e3", want: -1 },
  ])("$input → $want", ({ input, want }) => {
    expect(portValue(input)).toBe(want);
  });
});

describe("settings form", () => {
  const dto: SettingsDTO = {
    ...emptySettings,
    sshHost: "example.org", sshPort: 22, sshUser: "u", dbPort: 5432, dbName: "db", dbUser: "dbu",
    syncPhrase: "sixteen chars ok", trustedHostKey: "ssh-ed25519 AAAA", trustedHostAddr: "example.org:22",
    trustedFingerprint: "SHA256:x",
  };

  it("round-trips", () => {
    expect(fromForm(toForm(dto))).toEqual(dto);
  });

  it("shows unset ports as empty fields", () => {
    expect(toForm(emptySettings)).toMatchObject({ sshPort: "", dbPort: "" });
    expect(fromForm(toForm(emptySettings))).toEqual(emptySettings);
  });

  it("keeps garbage in a port distinct from an empty one", () => {
    const f = { ...toForm(emptySettings), sshPort: "abc" };
    expect(fromForm(f).sshPort).toBe(-1);
  });
});
