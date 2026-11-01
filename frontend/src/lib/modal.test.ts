// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { modal } from "./modal";

let app: HTMLElement;
let backup: HTMLButtonElement;

function add(html: string, parent: HTMLElement = app): HTMLElement {
  const wrap = document.createElement("div");
  wrap.innerHTML = html;
  const el = wrap.firstElementChild as HTMLElement;
  parent.append(el);
  return el;
}

beforeEach(() => {
  document.body.innerHTML = '<div id="app"><main><button id="settings">Settings</button><button id="backup">Backup</button></main></div>';
  app = document.getElementById("app")!;
  backup = document.getElementById("backup") as HTMLButtonElement;
  document.getElementById("settings")!.focus();
});

afterEach(() => {
  document.body.innerHTML = "";
});

describe("modal", () => {
  it("makes the rest of the window inert and moves the focus inside", () => {
    const dlg = add('<div class="backdrop"><input id="host"><button>OK</button></div>');
    const m = modal(dlg);

    expect(document.querySelector("main")!.hasAttribute("inert")).toBe(true);
    expect(dlg.hasAttribute("inert")).toBe(false);
    expect(document.activeElement?.id).toBe("host");

    m.destroy();
  });

  it("gives the window back and restores the focus when closed", () => {
    const dlg = add('<div class="backdrop"><button>OK</button></div>');
    modal(dlg).destroy();
    dlg.remove();

    expect(document.querySelectorAll("[inert]")).toHaveLength(0);
    expect(document.activeElement?.id).toBe("settings");
  });

  it("lets only the top modal take input", () => {
    const settings = add('<div class="backdrop"><input id="host"></div>');
    const a = modal(settings);
    const message = add('<div class="backdrop"><button id="ok">OK</button></div>');
    const b = modal(message);

    expect(settings.hasAttribute("inert")).toBe(true);
    expect(message.hasAttribute("inert")).toBe(false);
    expect(document.activeElement?.id).toBe("ok");

    b.destroy();
    message.remove();
    expect(settings.hasAttribute("inert")).toBe(false);
    expect(document.querySelector("main")!.hasAttribute("inert")).toBe(true);
    expect(document.activeElement?.id).toBe("host");

    a.destroy();
  });

  it("follows the opening order, not the closing order", () => {
    const first = add('<div class="backdrop"><button>1</button></div>');
    const a = modal(first);
    const second = add('<div class="backdrop"><button>2</button></div>');
    const b = modal(second);

    a.destroy();
    first.remove();
    expect(second.hasAttribute("inert")).toBe(false);
    expect(document.querySelector("main")!.hasAttribute("inert")).toBe(true);

    b.destroy();
    expect(document.querySelectorAll("[inert]")).toHaveLength(0);
  });

  it("takes the focus itself when it has nothing to focus", () => {
    const overlay = add('<div class="overlay">Synchronizing…</div>');
    const m = modal(overlay);

    expect(document.activeElement).toBe(overlay);
    expect(backup.closest("[inert]")).not.toBeNull();

    m.destroy();
  });

  it("moves the focus to the home element when nothing had the focus before", () => {
    const home = add('<button data-focus-home id="first">alpha</button>');
    (document.activeElement as HTMLElement | null)?.blur();
    const overlay = add('<div class="overlay">Connecting…</div>');
    modal(overlay).destroy();
    overlay.remove();

    expect(document.activeElement).toBe(home);
  });

  it("moves the focus to the home element when the previous one is gone", () => {
    const home = add('<button data-focus-home id="first">alpha</button>');
    const overlay = add('<div class="overlay">Connecting…</div>');
    const o = modal(overlay);
    const message = add('<div class="backdrop"><button>OK</button></div>');
    const m = modal(message);
    o.destroy();
    overlay.remove();
    m.destroy();
    message.remove();

    expect(document.activeElement).toBe(home);
  });

  it("prefers the previous focus to the home element", () => {
    add('<button data-focus-home id="first">alpha</button>');
    const dlg = add('<div class="backdrop"><button>OK</button></div>');
    modal(dlg).destroy();
    dlg.remove();

    expect(document.activeElement?.id).toBe("settings");
  });

  it("leaves an element inert that was inert before", () => {
    const aside = add("<aside inert></aside>");
    const m = modal(add('<div class="backdrop"><button>OK</button></div>'));
    m.destroy();

    expect(aside.hasAttribute("inert")).toBe(true);
  });
});
