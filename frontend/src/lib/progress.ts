// Progress of a transfer with the remote storage, from the sync:progress
// event (internal/adapter/wailsui/progress.go), as the overlay shows it.

/** Progress is the ProgressDTO of the event; direction "" ends the operation. */
export type Progress = { storage: string; direction: string; unit: string; done: number; total: number };

/** ProgressView is what the overlay shows for a transfer. */
export type ProgressView = {
  title: string;
  /** detail is "2.4 of 5.0 MB · 48%", "12 of 340 notes · 3%", or "Finishing…" once all is sent. */
  detail: string;
  /** warning is shown under the bar for Yandex Disk, "" otherwise. */
  warning: string;
  done: number;
  total: number;
};

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

function detail(p: Progress): string {
  if (p.done >= p.total) return "Finishing…";
  const percent = Math.floor((p.done * 100) / p.total);
  const amount = p.unit === "bytes" ? `${megabytes(p.done)} of ${megabytes(p.total)} MB` : `${p.done} of ${p.total} notes`;
  return `${amount} · ${percent}%`;
}

/** progressView describes p for the overlay; undefined without a transfer. restoring is the Restore download. */
export function progressView(p: Progress | undefined, restoring = false): ProgressView | undefined {
  if (!p || p.direction === "") return undefined;
  return {
    title: title(p, restoring),
    detail: detail(p),
    warning: p.storage === YANDEX_DISK ? SLOW : "",
    done: p.done,
    total: p.total,
  };
}
