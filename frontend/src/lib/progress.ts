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

function title(p: Progress): string {
  const yandex = p.storage === YANDEX_DISK;
  if (p.direction === "upload") return yandex ? "Uploading to Yandex Disk…" : "Uploading to the server…";
  return yandex ? "Downloading from Yandex Disk…" : "Downloading from the server…";
}

function detail(p: Progress): string {
  if (p.done >= p.total) return "Finishing…";
  const percent = Math.floor((p.done * 100) / p.total);
  const amount = p.unit === "bytes" ? `${megabytes(p.done)} of ${megabytes(p.total)} MB` : `${p.done} of ${p.total} notes`;
  return `${amount} · ${percent}%`;
}

/** progressView describes p for the overlay; undefined without a transfer. */
export function progressView(p: Progress | undefined): ProgressView | undefined {
  if (!p || p.direction === "") return undefined;
  return {
    title: title(p),
    detail: detail(p),
    warning: p.storage === YANDEX_DISK ? SLOW : "",
    done: p.done,
    total: p.total,
  };
}
