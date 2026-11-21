// The settings form keeps ports as text, so that garbage typed into a port
// reaches the Go validation (spec Review Focus 1) instead of turning into an
// empty field.
import type { SettingsDTO } from "./api";

export const emptySettings: SettingsDTO = {
  kind: "none", yandexClientId: "",
  googleClientId: "", googleClientSecret: "",
  sshHost: "", sshPort: 0, sshUser: "", sshPassword: "",
  dbHost: "", dbPort: 0, dbName: "", dbUser: "", dbPassword: "",
  syncPhrase: "", trustedFingerprint: "",
};

/** StorageKind is the value of SettingsDTO.kind: where the notes are synchronized. */
export type StorageKind = "none" | "yandex_disk" | "google_drive" | "ssh_postgres";

/** CloudKind is a storage the user signs in to. */
export type CloudKind = "yandex_disk" | "google_drive";

export function isCloud(kind: string): kind is CloudKind {
  return kind === "yandex_disk" || kind === "google_drive";
}

/**
 * Latest numbers the requests that change one shown value, such as the
 * account of the settings draft: begin returns a check that stays true only
 * until the next request begins, so a late answer to an older one is dropped.
 */
export class Latest {
  private last = 0;

  begin(): () => boolean {
    const mine = ++this.last;
    return () => mine === this.last;
  }
}

/** SignInPhase: idle, no sign-in; browser, waiting for the browser; manual, waiting for the code. */
export type SignInPhase = "idle" | "browser" | "manual";

export type SettingsForm = Omit<SettingsDTO, "sshPort" | "dbPort"> & { sshPort: string; dbPort: string };

/**
 * portValue converts a port field: empty is 0 (unset), a port 1-65535 is
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
  return { ...s, kind: s.kind || "none", sshPort: s.sshPort ? String(s.sshPort) : "", dbPort: s.dbPort ? String(s.dbPort) : "" };
}

export function fromForm(f: SettingsForm): SettingsDTO {
  return { ...f, sshPort: portValue(f.sshPort), dbPort: portValue(f.dbPort) };
}
