// Progress of a transfer with the remote storage, from the sync:progress
// event (internal/adapter/wailsui/progress.go), as the overlay shows it.

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
  /** warning is shown under the bar for Yandex Disk, "" otherwise. */
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

const YANDEX_DISK = "yandex_disk";
const SLOW = "This can take several minutes on a slow connection. Do not close Smartnote.";

/** megabytes are 10^6 bytes with one decimal. */
function megabytes(bytes: number): string {
  return (bytes / 1e6).toFixed(1);
}

/**
 * title says what the user is doing: an upload is a Save, a download is
 * "Downloading" only for the Restore the user started, else a synchronization.
 */
function title(p: Progress, restoring: boolean): string {
  const place = p.storage === YANDEX_DISK ? "Yandex Disk" : "the server";
  if (p.direction === "upload") return `Uploading to ${place}…`;
  return restoring ? `Downloading from ${place}…` : `Synchronizing with ${place}…`;
}

function amount(p: Progress): string {
  return p.unit === "bytes" ? `${megabytes(p.done)} of ${megabytes(p.total)} MB` : `${p.done} of ${p.total} notes`;
}

/**
 * progressView describes p for the overlay; undefined without a transfer.
 * restoring is the Restore download; step is the place of p in its process.
 */
export function progressView(p: Progress | undefined, restoring = false, step: Step = ONLY): ProgressView | undefined {
  if (!p || p.direction === "") return undefined;
  const count = Math.max(step.count, 1);
  const index = Math.min(Math.max(step.index, 0), count - 1);
  // The share done is num/den in whole numbers, so the percent is not off by a rounding.
  // With nothing to transfer the transfer is done.
  const [done, total] = p.total > 0 ? [Math.min(Math.max(p.done, 0), p.total), p.total] : [1, 1];
  const num = index * total + done;
  const den = count * total;
  const finished = index === count - 1 && p.done >= p.total;
  const text = finished ? "Finishing…" : `${amount(p)} · ${Math.floor((num * 100) / den)}%`;
  return {
    title: title(p, restoring),
    detail: count > 1 ? `Step ${index + 1} of ${count} · ${text}` : text,
    warning: p.storage === YANDEX_DISK ? SLOW : "",
    value: num / den,
  };
}
