// @vitest-environment happy-dom
import { flushSync, mount, tick, unmount } from "svelte";
import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import type { AccountDTO, SettingsDTO } from "../lib/api";
import { i18n } from "../lib/i18n.svelte";
import { TYPING_PAUSE_MS } from "../lib/settings";

// Settings talks to Go through the generated bindings; they are replaced by
// fakes whose answers the test controls.
const go = vi.hoisted(() => ({
  settings: {} as SettingsDTO,
  /** accounts are what Account answers by kind, as Go keeps the sign-ins of the form. */
  accounts: {} as Record<string, AccountDTO>,
  account: vi.fn(),
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
    Account: go.account,
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
i18n.use({ Language: async () => "en", SetLanguage: async () => {} });

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
  kind: "yandex_disk", yandexClientId: "",
  googleClientId: "", googleClientSecret: "",
  sshHost: "", sshPort: 0, sshUser: "", sshPassword: "", dbHost: "", dbPort: 0, dbName: "", dbUser: "", dbPassword: "",
  syncPhrase: "", trustedFingerprint: "",
};

let target: HTMLElement;
let screen: ReturnType<typeof mount>;
let onClose: Mock<() => void>;

const none: AccountDTO = { account: "", signedIn: false };
const signedIn = (account = ""): AccountDTO => ({ account, signedIn: true });

async function open(settings: Partial<SettingsDTO> = {}, accounts: Record<string, AccountDTO> = {}) {
  go.settings = { ...base, ...settings };
  go.accounts = { ...accounts };
  onClose = vi.fn<() => void>();
  screen = mount(Settings, { target, props: { onSaved: () => {}, onClose } });
  await settle();
}

beforeEach(() => {
  for (const f of [go.account, go.signIn, go.signInManual, go.submitCode, go.cancelSignIn, go.signOut, go.save, go.test, go.error, go.openURL]) f.mockReset();
  go.account.mockImplementation(async (s: SettingsDTO) => go.accounts[s.kind] ?? none);
  go.signOut.mockImplementation(async (kind: string) => {
    go.accounts[kind] = none;
  });
  go.cancelSignIn.mockResolvedValue(undefined);
  go.signInManual.mockResolvedValue(undefined);
  go.error.mockResolvedValue(undefined);
  target = document.createElement("div");
  document.body.append(target);
});

afterEach(async () => {
  await unmount(screen);
  target.remove();
  await i18n.set("en");
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
const field = (label: string) => target.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!;
const accountButtons = () => [...target.querySelectorAll('[data-testid="account"] button')].map((b) => b.textContent?.trim());
/** choose selects the storage labelled label, as a click does. */
const choose = async (label: string) => {
  radio(label).click();
  await settle();
};
/** edit types value into input and leaves it, which fires change. */
const edit = async (input: HTMLInputElement, value: string) => {
  input.value = value;
  input.dispatchEvent(new Event("input", { bubbles: true }));
  input.dispatchEvent(new Event("change", { bubbles: true }));
  await settle();
};
/** type types value into input without leaving it: input fires, change does not. */
const type = async (input: HTMLInputElement, value: string) => {
  input.value = value;
  input.dispatchEvent(new Event("input", { bubbles: true }));
  await settle();
};
/** pause waits until typing counts as paused. */
const pause = async () => {
  await new Promise((r) => setTimeout(r, TYPING_PAUSE_MS + 20));
  await settle();
};

/** pending returns a promise the test settles by hand. */
function pending<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => ((resolve = res), (reject = rej)));
  return { promise, resolve, reject };
}

describe("Settings storage choice", () => {
  it("offers the four storages in the order of the spec with the saved one chosen", async () => {
    await open({ kind: "ssh_postgres" });
    const labels = [...target.querySelectorAll('input[type="radio"]')].map((r) => r.closest("label")?.textContent?.trim());
    expect(labels).toEqual(["None (local only)", "Yandex Disk", "Google Drive", "SSH + PostgreSQL"]);
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

describe("Settings groups", () => {
  // WebKitGTK lays out fieldset+legend too tall on the first pass, so groups are plain sections.
  it("uses labelled sections instead of fieldset", async () => {
    await open({ kind: "ssh_postgres" });
    expect(target.querySelector("fieldset, legend")).toBeNull();
    const groups = [...target.querySelectorAll<HTMLElement>('section[role="group"]')];
    expect(groups.map((g) => g.querySelector(".legend")?.textContent?.trim())).toEqual(["Storage", "SSH server", "Database", "Sync phrase", "Keys"]);
    for (const g of groups) {
      const label = target.querySelector(`#${g.getAttribute("aria-labelledby")}`);
      expect(label, "aria-labelledby target").not.toBeNull();
      expect(label!.textContent).toBe(g.querySelector(".legend")!.textContent);
    }
  });
});

describe("Settings Yandex account", () => {
  it("shows the login and Sign out when signed in", async () => {
    await open({}, { yandex_disk: signedIn("ivan.petrov") });
    expect(has("Signed in as ivan.petrov")).toBe(true);
    expect(button("Sign out")).toBeDefined();
  });

  it("signs in again over a saved account", async () => {
    await open({}, { yandex_disk: signedIn("ivan.petrov") });
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
    await open({}, { yandex_disk: signedIn("ivan.petrov") });
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
    await open({}, { yandex_disk: signedIn() });
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

  it("switching language while waiting for the browser re-renders the account block", async () => {
    await open();
    go.signIn.mockReturnValue(pending<string>().promise);
    await press("Sign in with Yandex");

    await i18n.set("ru");
    await settle();

    expect(has("Ожидание браузера…")).toBe(true);
    expect(has("Waiting for the browser…")).toBe(false);
    expect(button("Ввести код вручную")).toBeDefined();
    expect([...target.querySelectorAll('[data-testid="account"] button')].map((b) => b.textContent?.trim())).toEqual(["Отмена", "Ввести код вручную"]);
    expect(target.querySelector("#settings-yandex")?.textContent).toBe("Аккаунт Яндекса");
    expect(button("Проверить подключение")!.disabled).toBe(true);
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
    await open({}, { yandex_disk: signedIn("ivan.petrov") });
    go.account.mockImplementation(async (s: SettingsDTO) => (s.yandexClientId === "" ? signedIn("ivan.petrov") : none));
    await edit(field("Own client ID"), "my-id");
    expect(go.account).toHaveBeenLastCalledWith(expect.objectContaining({ kind: "yandex_disk", yandexClientId: "my-id" }));
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
    expect(go.error).toHaveBeenCalledWith({ raw: "Yandex refused." });
    expect(has("Not signed in")).toBe(true);
  });

  it("signs out", async () => {
    await open({}, { yandex_disk: signedIn("ivan") });
    go.signOut.mockResolvedValue(undefined);
    await press("Sign out");
    expect(go.signOut).toHaveBeenCalledExactlyOnceWith("yandex_disk");
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
    for (const l of ["None (local only)", "Yandex Disk", "Google Drive", "SSH + PostgreSQL"]) expect(radio(l).disabled).toBe(true);
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
    expect(go.error).toHaveBeenCalledWith({ raw: "Client ID is required." });
    expect(has("Set your own client ID in Advanced")).toBe(true);
    expect(target.querySelector('input[aria-label="Own client ID"]')!.getAttribute("aria-invalid")).toBe("true");
    expect(target.querySelector("details")!.open).toBe(true);
  });

  it("marks the account block for a yandex_account validation error", async () => {
    await open();
    go.save.mockRejectedValue(uiError("validation", "Sign in to Yandex Disk first, or choose None (local only).", "yandex_account"));
    await press("OK");
    expect(target.querySelector('[data-testid="account"]')!.getAttribute("aria-invalid")).toBe("true");
    expect(go.error).toHaveBeenCalledWith({ raw: "Sign in to Yandex Disk first, or choose None (local only)." });
  });
});

describe("Settings Google Drive", () => {
  it("shows the Google account block with Sign in with Google and its logo", async () => {
    await open({ kind: "google_drive" });
    expect(target.querySelector("#settings-google")?.textContent).toBe("Google account");
    expect(has("Not signed in")).toBe(true);
    const b = button("Sign in with Google")!;
    expect(b).toBeDefined();
    const logo = b.querySelector("img")!;
    // The standard colour "G" as plain paths, which every WebView draws (no foreignObject).
    const svg = decodeURIComponent(logo.getAttribute("src")!);
    for (const colour of ["#EA4335", "#4285F4", "#FBBC05", "#34A853"]) expect(svg).toContain(colour);
    expect(svg).not.toMatch(/foreignObject/i);
    expect(logo.getAttribute("alt")).toBe("");
    // The logo comes first, then the text (Sign in with Google Branding Guidelines).
    expect(logo.nextElementSibling?.textContent).toBe("Sign in with Google");
    expect(button("Sign in with Yandex")).toBeUndefined();
    expect(go.account).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ kind: "google_drive" }));
  });

  it("signs in to Google without Enter code manually", async () => {
    await open({ kind: "google_drive" });
    go.signIn.mockReturnValue(pending<string>().promise);
    await press("Sign in with Google");
    expect(go.signIn).toHaveBeenCalledWith(expect.objectContaining({ kind: "google_drive" }));
    expect(has("Waiting for the browser…")).toBe(true);
    expect(accountButtons()).toEqual(["Cancel"]);
  });

  it("shows plain Signed in for a Google token without an account", async () => {
    await open({ kind: "google_drive" });
    go.signIn.mockResolvedValue("");
    await press("Sign in with Google");
    expect(has("Signed in")).toBe(true);
    expect(has("Signed in as")).toBe(false);
    expect(accountButtons()).toEqual(["Sign in again", "Sign out"]);
  });

  it("shows the error instead of a code field when the loopback is unavailable", async () => {
    await open({ kind: "google_drive" });
    go.signIn.mockRejectedValue(uiError("loopback_unavailable", "The sign-in page could not be received."));
    await press("Sign in with Google");
    expect(go.signInManual).not.toHaveBeenCalled();
    expect(codeField()).toBeNull();
    expect(go.error).toHaveBeenCalledWith({ raw: "The sign-in page could not be received." });
    expect(has("Not signed in")).toBe(true);
  });

  it("signs out of Google only", async () => {
    await open({ kind: "google_drive" }, { google_drive: signedIn() });
    await press("Sign out");
    expect(go.signOut).toHaveBeenCalledExactlyOnceWith("google_drive");
    expect(has("Not signed in")).toBe(true);
  });

  it("has the own client ID and secret in Advanced with the Google hint", async () => {
    await open({ kind: "google_drive", googleClientId: "1-abc.apps.googleusercontent.com", googleClientSecret: "GOCSPX-own" });
    expect(field("Own client ID").value).toBe("1-abc.apps.googleusercontent.com");
    expect(field("Own client secret").value).toBe("GOCSPX-own");
    expect(field("Own client secret").type).toBe("password");
    expect(has("Own client secret:")).toBe(true);
    expect(has("A client of another Google Cloud project has its own storage.")).toBe(true);
    expect(has("Data of another client ID is a separate, empty storage.")).toBe(false);

    const a = [...target.querySelectorAll("a")].find((l) => l.textContent?.trim() === "How to register")!;
    expect(a.getAttribute("href")).toBe("https://github.com/nikchis/smartnote/blob/master/docs/google-drive.md");
    a.click();
    expect(go.openURL).toHaveBeenCalledWith("https://github.com/nikchis/smartnote/blob/master/docs/google-drive.md");

    go.save.mockResolvedValue(undefined);
    await press("OK");
    expect(go.save).toHaveBeenCalledWith(expect.objectContaining({ kind: "google_drive", googleClientId: "1-abc.apps.googleusercontent.com", googleClientSecret: "GOCSPX-own" }));
  });

  it("names the Google block, button and fields in Russian", async () => {
    await open({ kind: "google_drive" });
    go.signIn.mockReturnValue(pending<string>().promise);
    await i18n.set("ru");
    await settle();
    expect(target.querySelector("#settings-google")?.textContent).toBe("Аккаунт Google");
    expect(button("Войти через Google")).toBeDefined();
    expect(field("Свой client ID")).not.toBeNull();
    expect(field("Свой client secret")).not.toBeNull();
    expect(has("У клиента другого проекта Google Cloud своё хранилище.")).toBe(true);

    await press("Войти через Google");
    expect(has("Ожидание браузера…")).toBe(true);
    expect(accountButtons()).toEqual(["Отмена"]);
  });

  it("marks the own client fields and opens Advanced for their validation errors", async () => {
    await open({ kind: "google_drive" });
    go.signIn.mockRejectedValue(uiError("validation", "Client ID is required.", "google_client_id"));
    await press("Sign in with Google");
    expect(has("Set your own client ID in Advanced")).toBe(true);
    expect(field("Own client ID").getAttribute("aria-invalid")).toBe("true");
    expect(target.querySelector("details")!.open).toBe(true);

    go.save.mockRejectedValue(uiError("validation", "Client secret is invalid.", "google_client_secret"));
    await press("OK");
    expect(field("Own client secret").getAttribute("aria-invalid")).toBe("true");
    expect(target.querySelector("details")!.open).toBe(true);
  });

  it("marks the account block for a google_account validation error", async () => {
    await open({ kind: "google_drive" });
    go.save.mockRejectedValue(uiError("validation", "Sign in to Google Drive first, or choose None (local only).", "google_account"));
    await press("OK");
    expect(target.querySelector('[data-testid="account"]')!.getAttribute("aria-invalid")).toBe("true");
  });
});

describe("Settings account of the draft", () => {
  it("account follows selected kind both ways without OK", async () => {
    await open({ kind: "google_drive" }, { google_drive: signedIn() });
    expect(go.account).toHaveBeenLastCalledWith(expect.objectContaining({ kind: "google_drive" }));
    expect(accountButtons()).toEqual(["Sign in again", "Sign out"]);
    expect(has("Signed in")).toBe(true);

    await choose("Yandex Disk");
    expect(go.account).toHaveBeenLastCalledWith(expect.objectContaining({ kind: "yandex_disk" }));
    expect(has("Not signed in")).toBe(true);
    expect(accountButtons()).toEqual(["Sign in with Yandex"]);

    await choose("Google Drive");
    expect(go.account).toHaveBeenLastCalledWith(expect.objectContaining({ kind: "google_drive" }));
    expect(has("Not signed in")).toBe(false);
    expect(accountButtons()).toEqual(["Sign in again", "Sign out"]);

    expect(go.account).toHaveBeenCalledTimes(3);
    expect(go.save).not.toHaveBeenCalled();
  });

  it("own client id change re-reads account", async () => {
    await open({ kind: "google_drive" }, { google_drive: signedIn() });
    go.account.mockResolvedValue(none);
    await edit(field("Own client ID"), "1-abc.apps.googleusercontent.com");
    expect(go.account).toHaveBeenLastCalledWith(expect.objectContaining({ kind: "google_drive", googleClientId: "1-abc.apps.googleusercontent.com" }));
    expect(has("Not signed in")).toBe(true);
    expect(accountButtons()).toEqual(["Sign in with Google"]);
  });

  // The effective Google client needs the ID and the secret together (usecase.EffectiveClient).
  it("own client secret change re-reads account", async () => {
    await open({ kind: "google_drive", googleClientId: "1-abc.apps.googleusercontent.com" });
    go.account.mockResolvedValue(signedIn());
    await edit(field("Own client secret"), "GOCSPX-own");
    expect(go.account).toHaveBeenLastCalledWith(expect.objectContaining({ googleClientId: "1-abc.apps.googleusercontent.com", googleClientSecret: "GOCSPX-own" }));
    expect(accountButtons()).toEqual(["Sign in again", "Sign out"]);
  });

  // The browser fires change only when the field is left: the secret that
  // completes the pair must show the sign-in while the field keeps the focus.
  it("re-reads the account once typing in an own client field pauses", async () => {
    await open({ kind: "google_drive" });
    await edit(field("Own client ID"), "1-abc.apps.googleusercontent.com");
    expect(has("Not signed in")).toBe(true);
    const reads = go.account.mock.calls.length;

    go.account.mockResolvedValue(signedIn("tester@example.invalid"));
    await type(field("Own client secret"), "GOCSPX-o");
    await type(field("Own client secret"), "GOCSPX-own");
    expect(go.account).toHaveBeenCalledTimes(reads);
    await pause();

    expect(go.account).toHaveBeenCalledTimes(reads + 1);
    expect(go.account).toHaveBeenLastCalledWith(expect.objectContaining({ googleClientId: "1-abc.apps.googleusercontent.com", googleClientSecret: "GOCSPX-own" }));
    expect(has("Signed in as tester@example.invalid")).toBe(true);
  });

  // A read that starts after the sign-in would outdate the sign-in's answer.
  it("leaving the field reads at once and drops the read of the pause", async () => {
    await open({ kind: "google_drive" });
    const reads = go.account.mock.calls.length;
    const login = pending<string>();
    go.signIn.mockReturnValue(login.promise);
    await edit(field("Own client secret"), "GOCSPX-own");
    expect(go.account).toHaveBeenCalledTimes(reads + 1);

    await press("Sign in with Google");
    await pause();
    expect(go.account).toHaveBeenCalledTimes(reads + 1);
    login.resolve("tester@example.invalid");
    await settle();
    expect(has("Signed in as tester@example.invalid")).toBe(true);
  });

  // Leaving the field fires change on the mousedown of the button under the
  // pointer: the button must still be there for the click that follows.
  it("signs in with the first click after an own client field changes", async () => {
    await open({ kind: "google_drive" });
    const answer = pending<AccountDTO>();
    go.account.mockReturnValueOnce(answer.promise);
    go.signIn.mockResolvedValue("");
    const input = field("Own client ID");
    input.focus();
    await edit(input, "1-abc.apps.googleusercontent.com");
    input.blur();
    await settle();

    expect(accountButtons()).toEqual(["Sign in with Google"]);
    await press("Sign in with Google");
    expect(go.signIn).toHaveBeenCalledOnce();
    expect(go.signIn).toHaveBeenCalledWith(expect.objectContaining({ googleClientId: "1-abc.apps.googleusercontent.com" }));

    // The read began before the sign-in: its answer no longer counts.
    answer.resolve(none);
    await settle();
    expect(accountButtons()).toEqual(["Sign in again", "Sign out"]);
  });

  it("keeps the focused button of the block while an own client field is re-read", async () => {
    await open({ kind: "yandex_disk" }, { yandex_disk: signedIn("ivan") });
    const answer = pending<AccountDTO>();
    go.account.mockReturnValueOnce(answer.promise);
    const input = field("Own client ID");
    input.focus();
    await edit(input, "my-id");
    // The keyboard moves on from the field to a button of the block.
    const signOut = button("Sign out")!;
    signOut.focus();
    await settle();
    expect(document.activeElement).toBe(signOut);

    answer.resolve(signedIn("ivan"));
    await settle();
    expect(signOut.isConnected).toBe(true);
    expect(document.activeElement).toBe(signOut);
  });

  it("does not ask for the account of a storage that is not a cloud", async () => {
    await open({ kind: "ssh_postgres" });
    await choose("None (local only)");
    expect(go.account).not.toHaveBeenCalled();
  });

  it("shows the selected kind without sign-in and sign-out buttons until the answer comes", async () => {
    const answer = pending<AccountDTO>();
    go.account.mockReturnValueOnce(answer.promise);
    await open({ kind: "google_drive" });
    expect(target.querySelector("#settings-google")?.textContent).toBe("Google account");
    expect(accountButtons()).toEqual([]);
    expect(has("Not signed in")).toBe(false);

    answer.resolve(signedIn());
    await settle();
    expect(accountButtons()).toEqual(["Sign in again", "Sign out"]);
  });

  describe("late account answer for old draft is ignored", () => {
    it("after a switch of the kind", async () => {
      const late = pending<AccountDTO>();
      go.account.mockReturnValueOnce(late.promise);
      await open({ kind: "google_drive" }, { yandex_disk: signedIn("ivan") });
      await choose("Yandex Disk");
      expect(has("Signed in as ivan")).toBe(true);

      late.resolve(none);
      await settle();
      expect(has("Signed in as ivan")).toBe(true);
      expect(accountButtons()).toEqual(["Sign in again", "Sign out"]);
      expect(target.querySelector("#settings-yandex")).not.toBeNull();
    });

    it("after a change of the own client ID", async () => {
      const late = pending<AccountDTO>();
      go.account.mockReturnValueOnce(late.promise);
      await open({ kind: "google_drive" });
      await edit(field("Own client ID"), "1-abc.apps.googleusercontent.com");
      expect(has("Not signed in")).toBe(true);

      late.resolve(signedIn());
      await settle();
      expect(has("Not signed in")).toBe(true);
      expect(accountButtons()).toEqual(["Sign in with Google"]);
    });

    it("after Sign out pressed while the read for a new own client ID runs", async () => {
      await open({ kind: "google_drive" }, { google_drive: signedIn() });
      const late = pending<AccountDTO>();
      go.account.mockReturnValueOnce(late.promise);
      await edit(field("Own client ID"), "1-abc.apps.googleusercontent.com");
      await press("Sign out");
      expect(go.signOut).toHaveBeenCalledExactlyOnceWith("google_drive");

      late.resolve(signedIn());
      await settle();
      expect(has("Not signed in")).toBe(true);
      expect(accountButtons()).toEqual(["Sign in with Google"]);
    });

    it("after a sign-out that ends once another kind is chosen", async () => {
      await open({ kind: "google_drive" }, { google_drive: signedIn(), yandex_disk: signedIn("ivan") });
      const out = pending<void>();
      go.signOut.mockReturnValueOnce(out.promise);
      await press("Sign out");
      await choose("Yandex Disk");
      expect(has("Signed in as ivan")).toBe(true);

      out.resolve();
      await settle();
      expect(has("Signed in as ivan")).toBe(true);
      expect(accountButtons()).toEqual(["Sign in again", "Sign out"]);
    });
  });

  it("sign out then switch kind shows other account", async () => {
    await open({ kind: "google_drive" }, { google_drive: signedIn(), yandex_disk: signedIn("ivan") });
    await press("Sign out");
    expect(has("Not signed in")).toBe(true);

    await choose("Yandex Disk");
    expect(has("Signed in as ivan")).toBe(true);
    expect(accountButtons()).toEqual(["Sign in again", "Sign out"]);

    go.save.mockResolvedValue(undefined);
    await press("OK");
    expect(go.signOut).toHaveBeenCalledExactlyOnceWith("google_drive");
    expect(go.save).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ kind: "yandex_disk" }));
  });

  it("pending sign-in survives kind switch", async () => {
    await open({ kind: "yandex_disk" });
    go.signIn.mockImplementation(async (s: SettingsDTO) => {
      go.accounts[s.kind] = signedIn("ivan");
      return "ivan";
    });
    await press("Sign in with Yandex");
    expect(has("Signed in as ivan")).toBe(true);

    await choose("Google Drive");
    expect(has("Not signed in")).toBe(true);
    await choose("Yandex Disk");
    expect(has("Signed in as ivan")).toBe(true);
    expect(go.save).not.toHaveBeenCalled();
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

  const advanced = () => target.querySelector("details")!;
  /** toggleAdvanced opens or closes Advanced as a click on its summary does. */
  const toggleAdvanced = async () => {
    advanced().querySelector("summary")!.click();
    await settle();
  };

  it("stays open after an error that is not about the own client", async () => {
    await open({ kind: "google_drive" });
    expect(advanced().open).toBe(false);
    await toggleAdvanced();
    expect(advanced().open).toBe(true);

    go.test.mockRejectedValue(uiError("validation", "Sign in to Google Drive first, or choose None (local only).", "google_account"));
    await press("Test connection");
    expect(go.error).toHaveBeenCalledOnce();
    expect(advanced().open).toBe(true);

    go.test.mockRejectedValue(uiError("internal", "boom"));
    await press("Test connection");
    expect(advanced().open).toBe(true);
  });

  it("keeps the user's choice when the storage kind changes", async () => {
    await open({ kind: "google_drive" });
    await toggleAdvanced();

    await choose("Yandex Disk");
    expect(advanced().open).toBe(true);
    await choose("None (local only)");
    await choose("Google Drive");
    expect(advanced().open).toBe(true);

    await toggleAdvanced();
    await choose("Yandex Disk");
    expect(advanced().open).toBe(false);
  });

  it("stays closed after an error once the user closed it", async () => {
    await open({ kind: "google_drive" });
    go.signIn.mockRejectedValue(uiError("validation", "Client ID is required.", "google_client_id"));
    await press("Sign in with Google");
    expect(advanced().open).toBe(true);
    await toggleAdvanced();
    expect(advanced().open).toBe(false);

    go.test.mockRejectedValue(uiError("validation", "Sign in to Google Drive first, or choose None (local only).", "google_account"));
    await press("Test connection");
    expect(advanced().open).toBe(false);
  });
});

describe("openExternal", () => {
  it("refuses links that are not https", async () => {
    const { openExternal } = await import("../lib/external");
    await expect(openExternal("file:///etc/passwd")).rejects.toThrow();
    expect(go.openURL).not.toHaveBeenCalled();
  });
});

describe("Settings sync phrase reveal", () => {
  const phraseInput = () => target.querySelector<HTMLInputElement>('section[aria-labelledby="settings-phrase"] input')!;
  const reveal = () => target.querySelector<HTMLButtonElement>('button[aria-label$="sync phrase"]')!;

  it("sync phrase reveal toggles input type", async () => {
    await open({ kind: "yandex_disk", syncPhrase: "secret phrase" });
    expect(reveal().getAttribute("aria-pressed")).toBe("false");
    expect(reveal().getAttribute("aria-label")).toBe("Show sync phrase");
    expect(phraseInput().type).toBe("password");
    expect(phraseInput().nextElementSibling).toBe(reveal());

    reveal().click();
    await settle();
    expect(phraseInput().type).toBe("text");
    expect(reveal().getAttribute("aria-pressed")).toBe("true");
    expect(reveal().getAttribute("aria-label")).toBe("Hide sync phrase");

    reveal().click();
    await settle();
    expect(phraseInput().type).toBe("password");
  });

  it("sync phrase hidden on reopen", async () => {
    await open({ kind: "yandex_disk", syncPhrase: "secret phrase" });
    reveal().click();
    await settle();
    expect(phraseInput().type).toBe("text");
    await unmount(screen);
    await open({ kind: "yandex_disk", syncPhrase: "secret phrase" });
    expect(phraseInput().type).toBe("password");
  });
});
