// The search bar of the Main screen (search spec §9.1): the query, the hits
// of the last answer and the status line under them.
import type { SearchHitDTO, SearchStatusDTO } from "./api";
import { parseError } from "./errors";

/** SearchApi is the part of SearchService the search bar uses. */
export type SearchApi = { Search(q: string): Promise<SearchHitDTO[] | null> };

/** MAX_HITS is the most hits shown; Go returns no more either. */
const MAX_HITS = 10;

/**
 * SearchBox holds the query and the hits shown for it. A search runs
 * delayMs after the last input; each one gets a number and only the answer to
 * the latest one is shown, whatever order the answers come in.
 */
export class SearchBox {
  query = $state("");
  hits: SearchHitDTO[] = $state([]);
  /** selected is the index of the chosen hit, -1 without hits. */
  selected = $state(-1);
  /** status is the last status of the index, undefined until one is known. */
  status: SearchStatusDTO | undefined = $state();
  /** answered is the query the hits answer, undefined while the answer is pending. */
  private answered: string | undefined = $state();
  private failed = $state(false);
  /** unavailable tells that the last search failed because the worker could not run it. */
  private unavailable = false;
  private seq = 0;
  private timer: ReturnType<typeof setTimeout> | undefined;

  constructor(
    private readonly api: SearchApi,
    private readonly delayMs = 150,
  ) {}

  /** input sets the query and searches it after the delay; a blank query clears the hits at once. */
  input(q: string): void {
    this.query = q;
    clearTimeout(this.timer);
    const n = ++this.seq;
    if (q.trim() === "") {
      this.show(q, [], false);
      return;
    }
    this.answered = undefined;
    this.timer = setTimeout(() => void this.run(q, n), this.delayMs);
  }

  /**
   * again searches the current query at once, shows the answer and returns
   * its hits; a failed search returns none. A jump calls it when the text of
   * a hit no longer matches the note (search spec §9.2).
   */
  again(): Promise<SearchHitDTO[]> {
    clearTimeout(this.timer);
    const n = ++this.seq;
    if (this.query.trim() === "") {
      this.show(this.query, [], false);
      return Promise.resolve([]);
    }
    return this.run(this.query, n);
  }

  move(delta: 1 | -1): void {
    if (this.hits.length === 0) return;
    this.selected = Math.min(Math.max(this.selected + delta, 0), this.hits.length - 1);
  }

  current(): SearchHitDTO | undefined {
    return this.hits[this.selected];
  }

  /**
   * follow takes a status from the search:status event. When the word search
   * is back (ready after loading or unavailable, or any ready status after a
   * search the worker could not run, code search_unavailable), it searches
   * the current query again: the last answer came from no worker or from an
   * index still being rebuilt (search spec §9.1, §11). Other failures are not
   * repeated. It also searches again once when the semantic search turns
   * ready after loading: the last answer had no semantic hits.
   */
  follow(s: SearchStatusDTO): void {
    const back = s.lexical === "ready" && (this.status?.lexical !== "ready" || this.unavailable);
    const model = s.semantic === "ready" && this.status?.semantic === "loading";
    this.status = s;
    if ((back || model) && this.query.trim() !== "") void this.again();
  }

  /**
   * seed takes the status read with Status() once at the start; an event that
   * came first is newer, so seed keeps it.
   */
  seed(s: SearchStatusDTO): void {
    this.status ??= s;
  }

  /**
   * statusLine is the line under the hits, "" when there is nothing to say.
   * One line is shown, the first that applies: Search unavailable (the last
   * search failed or the index cannot search), No matches (the answer to the
   * current query is empty), Semantic search is loading (the model loads and
   * no embedding is computed meanwhile), Indexing N of M (notes still wait for
   * their embeddings), Semantic search unavailable.
   */
  statusLine(): string {
    const s = this.status;
    if (this.failed || s?.lexical === "unavailable") return "Search unavailable";
    if (this.answered !== undefined && this.answered.trim() !== "" && this.hits.length === 0) return "No matches";
    if (!s) return "";
    if (s.semantic === "loading") return "Semantic search is loading";
    if (s.queued > 0) return `Indexing ${s.notes - s.queued} of ${s.notes}`;
    if (s.semantic === "unavailable") return "Semantic search unavailable";
    return "";
  }

  private async run(q: string, n: number): Promise<SearchHitDTO[]> {
    let hits: SearchHitDTO[] = [];
    let failed = false;
    let unavailable = false;
    try {
      hits = ((await this.api.Search(q)) ?? []).slice(0, MAX_HITS);
    } catch (e) {
      // Go logs the reason; the status line says the search failed.
      failed = true;
      unavailable = parseError(e).code === "search_unavailable";
    }
    if (n === this.seq) {
      this.show(q, hits, failed);
      this.unavailable = unavailable;
    }
    return hits;
  }

  private show(q: string, hits: SearchHitDTO[], failed: boolean): void {
    this.unavailable = false;
    this.answered = q;
    this.hits = hits;
    this.failed = failed;
    this.selected = hits.length > 0 ? 0 : -1;
  }
}

/** Piece is a part of a snippet, marked when it matched. */
export type Piece = { text: string; mark: boolean };

/**
 * highlight splits snippet by marks, UTF-16 ranges [start, end) from Go; the
 * marks may come unsorted or overlapping, and null is none. The pieces are
 * shown as text, never as HTML.
 */
export function highlight(snippet: string, marks: number[][] | null): Piece[] {
  const ranges = (marks ?? [])
    .map(([start, end]) => [Math.max(start, 0), Math.min(end, snippet.length)] as const)
    .filter(([start, end]) => start < end)
    .sort((a, b) => a[0] - b[0]);
  const pieces: Piece[] = [];
  let at = 0;
  for (const [start, end] of ranges) {
    if (end <= at) continue;
    if (start > at) pieces.push({ text: snippet.slice(at, start), mark: false });
    const from = Math.max(start, at);
    const last = pieces.at(-1);
    if (last?.mark && from === at) last.text += snippet.slice(from, end);
    else pieces.push({ text: snippet.slice(from, end), mark: true });
    at = end;
  }
  if (at < snippet.length || pieces.length === 0) pieces.push({ text: snippet.slice(at), mark: false });
  return pieces;
}

type Keys = Pick<KeyboardEvent, "key" | "code" | "ctrlKey" | "metaKey" | "altKey" | "shiftKey">;

/**
 * isFindShortcut tells whether e is Ctrl+F, or Cmd+F on macOS. The F is the
 * letter in a Latin layout or the F key in any other, as а in Russian.
 */
export function isFindShortcut(e: Keys, mac: boolean): boolean {
  const command = mac ? e.metaKey && !e.ctrlKey : e.ctrlKey && !e.metaKey;
  return command && !e.altKey && !e.shiftKey && (e.key.toLowerCase() === "f" || e.code === "KeyF");
}
