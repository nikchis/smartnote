// Text rules shared with internal/domain/text.go: lengths are counted in
// UTF-16 code units, which is also what the browser's maxlength counts.

/** Length limits of internal/domain/note.go. */
export const LIMITS = { name: 64, info: 8192, passwordMin: 6, passwordMax: 64, phraseMin: 16 } as const;

/** normalizeText turns CRLF into LF, as domain.NormalizeText does. */
export function normalizeText(s: string): string {
  return s.replaceAll("\r\n", "\n");
}

/** utf16Len is the length of a name or text after normalizeText. */
export function utf16Len(s: string): number {
  return normalizeText(s).length;
}

/** secretLen is the length of a password or the sync phrase after NFC, as domain.ValidatePassword counts it. */
export function secretLen(s: string): number {
  return s.normalize("NFC").length;
}
