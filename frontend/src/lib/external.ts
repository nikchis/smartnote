// Links that leave the application open in the system browser.
import { Browser } from "@wailsio/runtime";

/** YANDEX_DOCS describes how to register an own Yandex client ID. */
export const YANDEX_DOCS = "https://github.com/nikchis/smartnote/blob/master/docs/yandex-disk.md";

/** openExternal opens url in the default browser; the webview must not navigate away. */
export function openExternal(url: string): Promise<void> {
  return Browser.OpenURL(url);
}
