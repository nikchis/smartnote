// Errors of the Go services (internal/adapter/wailsui/errors.go).
import type { Msg } from "./i18n.svelte";

/**
 * UIError is the error a service call rejects with; code follows spec §3.5.
 * message is English; messages has the text in each language (spec §16.4).
 */
export type UIError = {
  code: string;
  message: string;
  messages?: { en: string; ru: string };
  field?: string;
  fingerprint?: string;
  expected?: string;
  got?: string;
  address?: string;
};

const optionalFields = ["field", "fingerprint", "expected", "got", "address"] as const;

/**
 * parseError reads the UIError JSON that Go puts in the message of a rejected
 * call; anything else becomes {code: "internal"} with its text.
 */
export function parseError(e: unknown): UIError {
  const text = e instanceof Error ? e.message : String(e);
  const parsed = parseJSON(text);
  if (!isRecord(parsed) || typeof parsed.code !== "string" || typeof parsed.message !== "string") {
    return { code: "internal", message: text };
  }

  const out: UIError = { code: parsed.code, message: parsed.message };
  for (const key of optionalFields) {
    const value = parsed[key];
    if (typeof value === "string") {
      out[key] = value;
    }
  }
  const messages = parsed.messages;
  if (isRecord(messages) && typeof messages.en === "string" && typeof messages.ru === "string") {
    out.messages = { en: messages.en, ru: messages.ru };
  }

  return out;
}

/** messageOf is the text of err for a dialog: both languages from Go, else the message as it is. */
export function messageOf(err: UIError): Msg {
  return err.messages ? { messages: err.messages } : { raw: err.message };
}

/** errorMsg is the text of a rejected call for a dialog. */
export function errorMsg(e: unknown): Msg {
  return messageOf(parseError(e));
}

function parseJSON(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}
