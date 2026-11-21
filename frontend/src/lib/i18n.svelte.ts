// The language of the interface (spec §16.4): the catalogs, the current
// language and the texts in it. Go keeps the choice (PrefsService), so the
// interface asks it at start and tells it each switch; main.ts connects it
// with use, tests connect a fake.
import { en } from "./i18n/en";
import { ru } from "./i18n/ru";

export type Lang = "en" | "ru";

/** Key names a text of the catalogs. */
export type Key = keyof typeof en;

export type Params = Record<string, string | number>;

/**
 * Msg is a text that may outlive the language it was made in, as a dialog
 * waiting in the queue: a key with its parameters, a message of Go in both
 * languages (UIError.messages), a technical text that has no translation
 * (SSH, PostgreSQL), or lines of other messages, empty ones left out.
 */
export type Msg = { key: Key; params?: Params } | { messages: { en: string; ru: string } } | { raw: string } | { lines: Msg[] };

/** msg is the message of key with params. */
export function msg(key: Key, params?: Params): Msg {
  return params ? { key, params } : { key };
}

/** PrefsApi is the part of PrefsService the language needs. */
export type PrefsApi = {
  Language(system: string): Promise<string>;
  SetLanguage(lang: string): Promise<void>;
};

const catalogs: Record<Lang, Record<Key, string>> = { en, ru };

/** PLURALS are the plural categories of a language in the order of the forms in its catalog. */
const PLURALS: Record<Lang, Intl.LDMLPluralRule[]> = { en: ["one", "other"], ru: ["one", "few", "many"] };

/** PLACEHOLDER is {name} or {name|form|form...}. */
const PLACEHOLDER = /\{(\w+)((?:\|[^|}]*)*)\}/g;

/** fromSystem is the language of a system locale: "ru" and "ru-…" are Russian, anything else English. */
function fromSystem(system: string): Lang {
  return /^ru(-|$)/i.test(system) ? "ru" : "en";
}

export class I18n {
  lang: Lang = $state("en");

  private prefs: PrefsApi | undefined;

  /** use connects the store of the choice; start and set need it. */
  use(prefs: PrefsApi): void {
    this.prefs = prefs;
  }

  /**
   * start takes the saved language, or the one of system while none is
   * saved; Go makes it current for its own texts. Without an answer from Go
   * (or without use) the language of system is shown.
   */
  async start(system: string): Promise<void> {
    try {
      if (!this.prefs) throw new Error("i18n: use was not called");
      const lang = await this.prefs.Language(system);
      this.lang = lang === "ru" ? "ru" : "en";
    } catch {
      this.lang = fromSystem(system);
    }
  }

  /** set switches to lang once Go has saved it; when Go fails the language stays and the error is thrown. */
  async set(lang: Lang): Promise<void> {
    if (!this.prefs) throw new Error("i18n: use was not called");
    await this.prefs.SetLanguage(lang);
    this.lang = lang;
  }

  /**
   * t is the text of key in the current language with params put in place of
   * {name}; {name|forms} gives the plural form for the number in name.
   * Parameter values are put in as they are.
   */
  t(key: Key, params: Params = {}): string {
    const lang = this.lang;
    return catalogs[lang][key].replace(PLACEHOLDER, (whole, name: string, forms: string) => {
      if (!Object.hasOwn(params, name)) return whole;
      const value = params[name];
      if (forms === "") return String(value);
      const list = forms.slice(1).split("|");
      const at = PLURALS[lang].indexOf(new Intl.PluralRules(lang).select(Number(value)));
      return list[at >= 0 ? at : list.length - 1];
    });
  }

  /** text is m in the current language. */
  text(m: Msg): string {
    if ("key" in m) return this.t(m.key, m.params);
    if ("messages" in m) return m.messages[this.lang];
    if ("lines" in m) return m.lines.map((line) => this.text(line)).filter((line) => line !== "").join("\n");
    return m.raw;
  }
}

// The catalogs do not import the bindings: the Wails runtime starts talking to
// Go when it is imported, which the tests of a component that only shows
// texts have no use for.
export const i18n = new I18n();
