// Progress of a transfer with the remote storage, from the sync:progress
// event (internal/adapter/wailsui/progress.go), as the overlay shows it, in
// the current language.
import { i18n } from "./i18n.svelte";

/** Progress is the ProgressDTO of the event; direction "" ends the operation. */
export type Progress = { storage: string; direction: string; unit: string; done: number; total: number };

/** ProgressView is what the overlay shows for a transfer. */
export type ProgressView = {
  title: string;
  /**
   * detail is "2.4 of 5.0 MB · 48%", "12 of 340 notes · 3%", or "Finishing…"
   * once all is sent; in a process of several transfers it starts with
   * "Step 2 of 3 · " and its percent is of the whole process.
   */
  detail: string;
  /** warning is shown under the bar for a cloud storage, "" otherwise. */
  warning: string;
  /** value is the share of the whole process done, from 0 to 1. */
  value: number;
};

/**
 * Step places a transfer in its process: index from 0 of count transfers,
 * each of which fills an equal share of the bar. An index beyond the last
 * counts as the last.
 */
export type Step = { index: number; count: number };

const ONLY: Step = { index: 0, count: 1 };

/**
 * Activity is what a transfer serves: "restore" is the Restore download the
 * user started, "backup" a transfer of a Backup process (also the one on
 * close), "sync" any other, as the reads of Connect.
 */
export type Activity = "sync" | "backup" | "restore";

/**
 * CLOUDS name the cloud storages; the names are the same in every language.
 * A transfer with a cloud can take minutes: the prototype found Google Drive
 * no faster than Yandex Disk (spec §13, П7), so both warn about it.
 */
const CLOUDS: Record<string, string> = { yandex_disk: "Yandex Disk", google_drive: "Google Drive" };

/** megabytes are 10^6 bytes with one decimal, written as the language writes it. */
function megabytes(bytes: number): string {
  return new Intl.NumberFormat(i18n.lang, { minimumFractionDigits: 1, maximumFractionDigits: 1, useGrouping: false }).format(bytes / 1e6);
}

/**
 * title says what the user is doing: an upload is a Backup, a download is
 * "Downloading" only for the Restore the user started, else a synchronization.
 * In Russian the reads of a Backup are named after the Backup too, and the
 * Backup and the Restore are named by their nouns (spec §16.4, glossary).
 */
function title(p: Progress, activity: Activity): string {
  const provider = CLOUDS[p.storage];
  if (p.direction === "upload") return provider ? i18n.t("progress.uploadCloud", { provider }) : i18n.t("progress.uploadServer");
  switch (activity) {
    case "restore":
      return provider ? i18n.t("progress.downloadCloud", { provider }) : i18n.t("progress.downloadServer");
    case "backup":
      return provider ? i18n.t("progress.backupReadCloud", { provider }) : i18n.t("progress.backupReadServer");
    case "sync":
      return provider ? i18n.t("progress.syncCloud", { provider }) : i18n.t("progress.syncServer");
  }
}

function amount(p: Progress): string {
  return p.unit === "bytes"
    ? i18n.t("progress.megabytes", { done: megabytes(p.done), total: megabytes(p.total) })
    : i18n.t("progress.notes", { done: p.done, total: p.total });
}

/**
 * progressView describes p for the overlay; undefined without a transfer.
 * activity is what the transfer serves; step is the place of p in its process.
 */
export function progressView(p: Progress | undefined, activity: Activity = "sync", step: Step = ONLY): ProgressView | undefined {
  if (!p || p.direction === "") return undefined;
  const count = Math.max(step.count, 1);
  const index = Math.min(Math.max(step.index, 0), count - 1);
  // The share done is num/den in whole numbers, so the percent is not off by a rounding.
  // With nothing to transfer the transfer is done.
  const [done, total] = p.total > 0 ? [Math.min(Math.max(p.done, 0), p.total), p.total] : [1, 1];
  const num = index * total + done;
  const den = count * total;
  const finished = index === count - 1 && p.done >= p.total;
  const text = finished ? i18n.t("progress.finishing") : `${amount(p)} · ${Math.floor((num * 100) / den)}%`;
  return {
    title: title(p, activity),
    detail: count > 1 ? i18n.t("progress.step", { index: index + 1, count, text }) : text,
    warning: Object.hasOwn(CLOUDS, p.storage) ? i18n.t("progress.slow") : "",
    value: num / den,
  };
}
