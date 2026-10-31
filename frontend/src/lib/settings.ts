// The settings form keeps ports as text, so that garbage typed into a port
// reaches the Go validation (spec Review Focus 1) instead of turning into an
// empty field.
import type { SettingsDTO } from "./api";

export const emptySettings: SettingsDTO = {
  sshHost: "", sshPort: 0, sshUser: "", sshPassword: "",
  dbHost: "", dbPort: 0, dbName: "", dbUser: "", dbPassword: "",
  syncPhrase: "", trustedHostKey: "", trustedHostAddr: "", trustedFingerprint: "",
};

export type SettingsForm = Omit<SettingsDTO, "sshPort" | "dbPort"> & { sshPort: string; dbPort: string };

/**
 * portValue converts a port field: empty is 0 (unset), an integer is itself
 * (Go checks the range), anything else is -1, which Go rejects as invalid.
 */
export function portValue(s: string): number {
  const t = s.trim();
  if (t === "") return 0;
  return /^-?\d+$/.test(t) ? Number(t) : -1;
}

export function toForm(s: SettingsDTO): SettingsForm {
  return { ...s, sshPort: s.sshPort ? String(s.sshPort) : "", dbPort: s.dbPort ? String(s.dbPort) : "" };
}

export function fromForm(f: SettingsForm): SettingsDTO {
  return { ...f, sshPort: portValue(f.sshPort), dbPort: portValue(f.dbPort) };
}
