// The settings form keeps ports as text, so that garbage typed into a port
// reaches the Go validation (spec Review Focus 1) instead of turning into an
// empty field.
import type { SettingsDTO } from "./api";

export const emptySettings: SettingsDTO = {
  sshHost: "", sshPort: 0, sshUser: "", sshPassword: "",
  dbHost: "", dbPort: 0, dbName: "", dbUser: "", dbPassword: "",
  syncPhrase: "", trustedFingerprint: "",
};

export type SettingsForm = Omit<SettingsDTO, "sshPort" | "dbPort"> & { sshPort: string; dbPort: string };

/**
 * portValue converts a port field: empty is 0 (unset), a port 1–65535 is
 * itself, anything else is -1, which Go rejects as invalid. Other numbers
 * would not reach the validation intact: 0 means unset, and a long one fails
 * JSON decoding in Go or turns into null, that is 0.
 */
export function portValue(s: string): number {
  const t = s.trim();
  if (t === "") return 0;
  const n = /^\d{1,5}$/.test(t) ? Number(t) : 0;
  return n >= 1 && n <= 65535 ? n : -1;
}

export function toForm(s: SettingsDTO): SettingsForm {
  return { ...s, sshPort: s.sshPort ? String(s.sshPort) : "", dbPort: s.dbPort ? String(s.dbPort) : "" };
}

export function fromForm(f: SettingsForm): SettingsDTO {
  return { ...f, sshPort: portValue(f.sshPort), dbPort: portValue(f.dbPort) };
}
