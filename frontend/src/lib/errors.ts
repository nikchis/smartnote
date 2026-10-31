// Errors of the Go services (internal/adapter/wailsui/errors.go).

/** UIError is the error a service call rejects with; code follows spec §3.5. */
export type UIError = {
  code: string;
  message: string;
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

  return out;
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
