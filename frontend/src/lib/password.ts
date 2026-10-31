import { LIMITS, secretLen } from "./text";

/**
 * passwordPairOK reports whether a new password and its confirmation may be
 * submitted: 6–64 characters after NFC and equal after NFC, as domain checks them.
 */
export function passwordPairOK(password: string, confirm: string): boolean {
  const n = secretLen(password);
  return n >= LIMITS.passwordMin && n <= LIMITS.passwordMax && password.normalize("NFC") === confirm.normalize("NFC");
}
