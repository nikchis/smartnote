// @vitest-environment happy-dom
import { flushSync, mount, tick, unmount } from "svelte";
import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import type { SettingsDTO } from "../lib/api";

// Settings talks to Go through the generated bindings; they are replaced by
// fakes whose answers the test controls.
const go = vi.hoisted(() => ({
  settings: {} as SettingsDTO,
  signIn: vi.fn(),
  signInManual: vi.fn(),
  submitCode: vi.fn(),
  cancelSignIn: vi.fn(),
  signOut: vi.fn(),
  save: vi.fn(),
  test: vi.fn(),
  error: vi.fn(),
  openURL: vi.fn(),
}));

vi.mock("../lib/api", () => ({
  ConnectionService: {
    Load: async () => go.settings,
    SignIn: go.signIn,
    SignInManual: go.signInManual,
    SubmitCode: go.submitCode,
    CancelSignIn: go.cancelSignIn,
    SignOut: go.signOut,
    Save: go.save,
    Test: go.test,
  },
  KeyService: { HasKey: async () => true },
}));
vi.mock("../lib/dialogs.svelte", () => ({
  dialogs: { error: go.error, info: vi.fn(async () => {}), ask: vi.fn(async () => true) },
}));
vi.mock("@wailsio/runtime", () => ({ Browser: { OpenURL: go.openURL } }));

const { default: Settings } = await import("./Settings.svelte");

const settle = async () => {
  for (let i = 0; i < 3; i++) {
    await new Promise((r) => setTimeout(r, 0));
    await tick();
    flushSync();
  }
};

/** uiError is how a rejected Go call looks to parseError. */
const uiError = (code: string, message = code, field?: string) => new Error(JSON.stringify({ code, message, field }));

const base: SettingsDTO = {
  kind: "yandex_disk", yandexClientId: "", yandexAccount: "", yandexSignedIn: false,
  sshHost: "", sshPort: 0, sshUser: "", sshPassword: "", dbHost: "", dbPort: 0, dbName: "", dbUser: "", dbPassword: "",
  syncPhrase: "", trustedFingerprint: "",
};

let target: HTMLElement;
let screen: ReturnType<typeof mount>;
let onClose: Mock<() => void>;

async function open(settings: Partial<SettingsDTO> = {}) {
  go.settings = { ...base, ...settings };
  onClose = vi.fn<() => void>();
  screen = mount(Settings, { target, props: { onSaved: () => {}, onClose } });
  await settle();
}

beforeEach(() => {
  for (const f of [go.signIn, go.signInManual, go.submitCode, go.cancelSignIn, go.signOut, go.save, go.test, go.error, go.openURL]) f.mockReset();
  go.cancelSignIn.mockResolvedValue(undefined);
  go.signInManual.mockResolvedValue(undefined);
  go.error.mockResolvedValue(undefined);
  target = document.createElement("div");
  document.body.append(target);
});

afterEach(async () => {
  await unmount(screen);
  target.remove();
});

const button = (name: string) => [...target.querySelectorAll("button")].find((b) => b.textContent?.trim() === name);
const press = async (name: string) => {
  const b = button(name);
  expect(b, `button ${name}`).toBeDefined();
  b!.click();
  await settle();
};
const radio = (label: string) => [...target.querySelectorAll<HTMLInputElement>('input[type="radio"]')].find((r) => r.closest("label")?.textContent?.trim() === label)!;
const has = (text: string) => target.textContent!.includes(text);
const codeField = () => target.querySelector<HTMLInputElement>('input[aria-label="Code"]');

/** pending returns a promise the test settles by hand. */
function pending<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => ((resolve = res), (reject = rej)));
  return { promise, resolve, reject };
}

describe("Settings storage choice", () => {
  it("offers the three storages with the saved one chosen", async () => {
    await open({ kind: "ssh_postgres" });
    expect(radio("None (local only)")).toBeDefined();
    expect(radio("Yandex Disk")).toBeDefined();
    expect(radio("SSH + PostgreSQL").checked).toBe(true);
  });

  it("shows SSH, Database and Keys only for SSH + PostgreSQL", async () => {
    await open({ kind: "ssh_postgres" });
    expect(has("SSH server")).toBe(true);
    expect(has("Database")).toBe(true);
    expect(has("Keys")).toBe(true);

    radio("Yandex Disk").click();
    await settle();
    expect(has("SSH server") || has("Database") || has("Keys")).toBe(false);
    expect(has("Sync phrase")).toBe(true);
    expect(has("Not signed in")).toBe(true);
  });

  it("shows nothing but the choice for None, and no Test", async () => {
    await open({ kind: "none" });
    expect(has("SSH server") || has("Sync phrase") || has("Not signed in")).toBe(false);
    expect(button("Test connection")!.disabled).toBe(true);
    expect(button("OK")!.disabled).toBe(false);
  });

  it("saves the chosen kind", async () => {
    await open({ kind: "none" });
    radio("Yandex Disk").click();
    await settle();
    go.save.mockResolvedValue(undefined);
    await press("OK");
    expect(go.save).toHaveBeenCalledWith(expect.objectContaining({ kind: "yandex_disk" }));
  });
});

describe("Settings Yandex account", () => {
  it("shows the login and Sign out when signed in", async () => {
    await open({ yandexSignedIn: true, yandexAccount: "ivan.petrov" });
    expect(has("Signed in as ivan.petrov")).toBe(true);
    expect(button("Sign out")).toBeDefined();
  });

  it("signs in again over a saved account", async () => {
    await open({ yandexSignedIn: true, yandexAccount: "ivan.petrov" });
    const signIn = pending<string>();
    go.signIn.mockReturnValue(signIn.promise);
    await press("Sign in again");

    expect(go.signIn).toHaveBeenCalledWith(expect.objectContaining({ kind: "yandex_disk" }));
    expect(has("Waiting for the browser…")).toBe(true);
    expect(button("OK")!.disabled).toBe(true);

    signIn.resolve("ivan.new");
    await settle();
    expect(has("Signed in as ivan.new")).toBe(true);
    go.save.mockResolvedValue(undefined);
    await press("OK");
    expect(go.save).toHaveBeenCalledOnce();
  });

  it("keeps the saved account when signing in again is canceled", async () => {
    await open({ yandexSignedIn: true, yandexAccount: "ivan.petrov" });
    const signIn = pending<string>();
    go.signIn.mockReturnValue(signIn.promise);
    await press("Sign in again");
    await press("Cancel");
    signIn.reject(uiError("canceled"));
    await settle();
    expect(has("Signed in as ivan.petrov")).toBe(true);
    expect(go.error).not.toHaveBeenCalled();
  });

  it("shows plain Signed in when the login is empty", async () => {
    await open({ yandexSignedIn: true });
    expect(target.textContent).toContain("Signed in");
    expect(has("Signed in as")).toBe(false);
  });

  it("waits for the browser with Cancel and Enter code manually", async () => {
    await open();
    const signIn = pending<string>();
    go.signIn.mockReturnValue(signIn.promise);
    await press("Sign in with Yandex");

    expect(go.signIn).toHaveBeenCalledWith(expect.objectContaining({ kind: "yandex_disk" }));
    expect(has("Waiting for the browser…")).toBe(true);
    expect(button("Cancel")).toBeDefined();
    expect(button("Enter code manually")).toBeDefined();
    expect(button("OK")!.disabled).toBe(true);
    expect(button("Test connection")!.disabled).toBe(true);
  });

  it("shows the login after a successful sign-in", async () => {
    await open();
    go.signIn.mockResolvedValue("ivan.petrov");
    await press("Sign in with Yandex");
    expect(has("Signed in as ivan.petrov")).toBe(true);
    expect(button("OK")!.disabled).toBe(false);
  });

  it("passes the own client ID with the form", async () => {
    await open();
    go.signIn.mockResolvedValue("ivan");
    const input = target.querySelector<HTMLInputElement>('input[aria-label="Own client ID"]')!;
    input.value = "my-id";
    input.dispatchEvent(new Event("input", { bubbles: true }));
    await press("Sign in with Yandex");
    expect(go.signIn).toHaveBeenCalledWith(expect.objectContaining({ yandexClientId: "my-id" }));
  });

  it("shows Not signed in after the own client ID changes", async () => {
    await open({ yandexSignedIn: true, yandexAccount: "ivan.petrov" });
    const input = target.querySelector<HTMLInputElement>('input[aria-label="Own client ID"]')!;
    input.value = "my-id";
    input.dispatchEvent(new Event("input", { bubbles: true }));
    await settle();
    expect(has("Signed in as")).toBe(false);
    expect(has("Not signed in")).toBe(true);
    expect(button("Sign in with Yandex")).toBeDefined();
  });

  it("goes straight to the code field when the loopback is unavailable", async () => {
    await open();
    go.signIn.mockRejectedValue(uiError("loopback_unavailable"));
    await press("Sign in with Yandex");
    expect(go.signInManual).toHaveBeenCalledOnce();
    expect(codeField()).not.toBeNull();
    expect(go.error).not.toHaveBeenCalled();
  });

  it("switches to the code field with Enter code manually", async () => {
    await open();
    const signIn = pending<string>();
    go.signIn.mockReturnValue(signIn.promise);
    await press("Sign in with Yandex");
    await press("Enter code manually");
    expect(go.cancelSignIn).toHaveBeenCalledOnce();
    expect(go.signInManual).toHaveBeenCalledOnce();
    expect(codeField()).not.toBeNull();

    // The first sign-in ends with "canceled"; it must not drop the code field.
    signIn.reject(uiError("canceled"));
    await settle();
    expect(codeField()).not.toBeNull();
  });

  it("submits the code", async () => {
    await open();
    go.signIn.mockRejectedValue(uiError("loopback_unavailable"));
    go.submitCode.mockResolvedValue("ivan.petrov");
    await press("Sign in with Yandex");
    codeField()!.value = "abc123";
    codeField()!.dispatchEvent(new Event("input", { bubbles: true }));
    await settle();
    await press("Submit");
    expect(go.submitCode).toHaveBeenCalledWith("abc123");
    expect(has("Signed in as ivan.petrov")).toBe(true);
  });

  it("returns silently to Not signed in when canceled", async () => {
    await open();
    const signIn = pending<string>();
    go.signIn.mockReturnValue(signIn.promise);
    await press("Sign in with Yandex");
    await press("Cancel");
    expect(go.cancelSignIn).toHaveBeenCalledOnce();
    signIn.reject(uiError("canceled"));
    await settle();
    expect(has("Not signed in")).toBe(true);
    expect(go.error).not.toHaveBeenCalled();
  });

  it("shows other sign-in errors", async () => {
    await open();
    go.signIn.mockRejectedValue(uiError("auth", "Yandex refused."));
    await press("Sign in with Yandex");
    expect(go.error).toHaveBeenCalledWith("Yandex refused.");
    expect(has("Not signed in")).toBe(true);
  });

  it("signs out", async () => {
    await open({ yandexSignedIn: true, yandexAccount: "ivan" });
    go.signOut.mockResolvedValue(undefined);
    await press("Sign out");
    expect(go.signOut).toHaveBeenCalledOnce();
    expect(has("Not signed in")).toBe(true);
  });

  it("cancels the sign-in when the dialog is closed", async () => {
    await open();
    go.signIn.mockReturnValue(pending<string>().promise);
    await press("Sign in with Yandex");
    // The dialog's own Cancel is the last one; the sign-in Cancel comes first.
    [...target.querySelectorAll("button")].filter((b) => b.textContent?.trim() === "Cancel").at(-1)!.click();
    await settle();
    expect(go.cancelSignIn).toHaveBeenCalledOnce();
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("disables the storage choice during a sign-in", async () => {
    await open();
    go.signIn.mockReturnValue(pending<string>().promise);
    await press("Sign in with Yandex");
    for (const l of ["None (local only)", "Yandex Disk", "SSH + PostgreSQL"]) expect(radio(l).disabled).toBe(true);
  });

  it("ignores a late sign-in success after Enter code manually", async () => {
    await open();
    const signIn = pending<string>();
    go.signIn.mockReturnValue(signIn.promise);
    await press("Sign in with Yandex");
    await press("Enter code manually");
    signIn.resolve("ivan");
    await settle();
    expect(codeField()).not.toBeNull();
    expect(has("Signed in as")).toBe(false);
  });

  it("does not call CancelSignIn when closing without a sign-in", async () => {
    await open();
    await press("Cancel");
    expect(go.cancelSignIn).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("shows the hint and marks the field when the build has no client ID", async () => {
    await open({ yandexClientId: "" });
    go.signIn.mockRejectedValue(uiError("validation", "Client ID is required.", "yandex_client_id"));
    await press("Sign in with Yandex");
    expect(go.error).toHaveBeenCalledWith("Client ID is required.");
    expect(has("Set your own client ID in Advanced")).toBe(true);
    expect(target.querySelector('input[aria-label="Own client ID"]')!.getAttribute("aria-invalid")).toBe("true");
    expect(target.querySelector("details")!.open).toBe(true);
  });

  it("marks the account block for a yandex_account validation error", async () => {
    await open();
    go.save.mockRejectedValue(uiError("validation", "Sign in to Yandex Disk first, or choose None (local only).", "yandex_account"));
    await press("OK");
    expect(target.querySelector('[data-testid="account"]')!.getAttribute("aria-invalid")).toBe("true");
    expect(go.error).toHaveBeenCalledWith("Sign in to Yandex Disk first, or choose None (local only).");
  });
});

describe("Settings Advanced", () => {
  it("explains that another client ID is a separate storage", async () => {
    await open();
    expect(has("Data of another client ID is a separate, empty storage.")).toBe(true);
    expect(has("Own client ID:")).toBe(true);
  });

  it("opens How to register in the browser", async () => {
    await open();
    const a = [...target.querySelectorAll("a")].find((l) => l.textContent?.trim() === "How to register")!;
    expect(a.getAttribute("href")).toBe("https://github.com/nikchis/smartnote/blob/master/docs/yandex-disk.md");
    a.click();
    expect(go.openURL).toHaveBeenCalledWith("https://github.com/nikchis/smartnote/blob/master/docs/yandex-disk.md");
  });
});

describe("openExternal", () => {
  it("refuses links that are not https", async () => {
    const { openExternal } = await import("../lib/external");
    await expect(openExternal("file:///etc/passwd")).rejects.toThrow();
    expect(go.openURL).not.toHaveBeenCalled();
  });
});
