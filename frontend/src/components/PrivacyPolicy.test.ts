// @vitest-environment happy-dom
import { flushSync, mount, unmount } from "svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "../lib/i18n.svelte";
import en from "../lib/privacy/en.html?raw";
import ru from "../lib/privacy/ru.html?raw";

const opened = vi.hoisted(() => [] as string[]);
vi.mock("../lib/external", () => ({
  openExternal: async (url: string) => {
    opened.push(url);
  },
}));

const { default: PrivacyPolicy } = await import("./PrivacyPolicy.svelte");

i18n.use({ Language: async () => "en", SetLanguage: async () => {} });

let target: HTMLElement;
let view: ReturnType<typeof mount>;
let closed: number;

beforeEach(() => {
  opened.length = 0;
  closed = 0;
  target = document.createElement("div");
  document.body.append(target);
  view = mount(PrivacyPolicy, { target, props: { onClose: () => closed++ } });
  flushSync();
});

afterEach(async () => {
  await unmount(view);
  target.remove();
  await i18n.set("en");
});

const links = (html: string) => [...new DOMParser().parseFromString(html, "text/html").querySelectorAll("a")];

describe("PrivacyPolicy", () => {
  it("shows the policy in the current language", async () => {
    expect(target.querySelector("h2#privacy-title")?.textContent).toBe("Privacy Policy");
    expect(target.textContent).toContain("Google user data");
    await i18n.set("ru");
    flushSync();
    expect(target.querySelector("h2#privacy-title")?.textContent).toBe("Политика конфиденциальности");
    expect(target.textContent).toContain("Данные пользователя Google");
    expect(target.textContent).not.toContain("Google user data");
  });

  it("links open in the browser", () => {
    const a = target.querySelector<HTMLAnchorElement>('a[href^="https://developers.google.com/"]')!;
    const click = new MouseEvent("click", { bubbles: true, cancelable: true });
    a.dispatchEvent(click);
    expect(click.defaultPrevented).toBe(true);
    expect(opened).toEqual([a.getAttribute("href")]);
  });

  it("middle click opens the link in the browser too", () => {
    const a = target.querySelector<HTMLAnchorElement>('a[href^="https://developers.google.com/"]')!;
    const aux = new MouseEvent("auxclick", { bubbles: true, cancelable: true, button: 1 });
    a.dispatchEvent(aux);
    expect(aux.defaultPrevented).toBe(true);
    expect(opened).toEqual([a.getAttribute("href")]);
  });

  it("the policy text can be scrolled from the keyboard", () => {
    const policy = target.querySelector<HTMLElement>(".policy")!;
    expect(policy.tabIndex).toBe(0);
    expect(policy.getAttribute("aria-labelledby")).toBe("privacy-title");
  });

  it("every link is absolute https", () => {
    for (const html of [en, ru]) {
      expect(links(html).length).toBeGreaterThan(0);
      for (const a of links(html)) expect(a.getAttribute("href")).toMatch(/^https:\/\//);
    }
  });

  it("ru link to the English version opens the site", async () => {
    await i18n.set("ru");
    flushSync();
    const a = [...target.querySelectorAll("a")].find((x) => x.textContent === "английская версия")!;
    a.click();
    expect(opened).toEqual(["https://nikchis.github.io/smartnote/privacy.html"]);
  });

  it("closes on Escape and Close", () => {
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    expect(closed).toBe(1);
    const close = [...target.querySelectorAll("button")].find((b) => b.textContent?.trim() === "Close")!;
    close.click();
    expect(closed).toBe(2);
  });

  it("shows the effective date in both languages", () => {
    expect(en).toContain("Effective November 21, 2026");
    expect(ru).toContain("Действует с 21 ноября 2026 г.");
    expect(target.textContent).toContain("Effective November 21, 2026");
  });

  it("has no dashes", () => {
    for (const html of [en, ru]) expect(html).not.toMatch(/[–—]/);
  });

  it("contains the Limited Use statement", () => {
    expect(en).toContain("Limited Use requirements");
    expect(ru).toContain("Ограниченное использование");
    expect(ru).toContain("https://developers.google.com/terms/api-services-user-data-policy");
  });
});
