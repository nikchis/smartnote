**English** | [Русский](search-maintenance.ru.md)

# Search maintenance

Notes for developers who change the search: the storage format, how the application restarts the
worker, what the worker may write to the log, and the checks outside the usual test run. How
the search looks to the user is in [Search](search.md); the CBOR samples of the worker protocol are
described in [search/testdata/protocol/README.md](../search/testdata/protocol/README.md), the model
preparation in [tools/model/README.md](../tools/model/README.md).

- Storage format 2 is one-way. `internal/adapter/sqlite` migrates a format 1 storage to 2 (table
  `note_embedding`) at the first unlock, after the password check, in one transaction; 2.0.0 and
  2.0.1 then refuse it with "Data was created by a newer version of Smartnote.". The only way back
  is Backup to the server with the new version and Restore into a new storage of 2.0.x
  ([Installation and upgrade](install.md#upgrading-from-20x)).
- Restarts of the worker (`internal/adapter/searchworker/backoff.go`): pauses 1, 2, 4, 8, 16 s; the
  engine gives up after 5 failed restarts within 10 minutes (at the sixth failure) and the search
  stays unavailable until the next start. While no worker is ready, and when a search times out,
  `Search` fails with `usecase.ErrSearchUnavailable` (interface: `Search unavailable`, never
  `No matches`); three search timeouts in a row restart the worker. The search bar searches the
  current query again when the word search becomes ready; `usecase.Search` holds the statuses
  while a rebuild is queued (until its marker command `rebuilt` is reached), so that query never
  meets the empty index of a new worker.
- What the worker writes reaches `smartnote.log` only as a closed vocabulary within a budget per
  run of the application (`internal/adapter/searchworker/stderr.go`, `workerOutput` of the
  `Engine`; restarts do not renew it): a stderr line is logged when it matches a form there, with
  no free text (OS error messages, ONNX Runtime locations and files outside the worker are left
  out; a panic names its file only from `workerSourceFiles`), otherwise as
  `search worker stderr: unrecognized line` with its length; the `msg` of an error reply only when
  it is in `knownErrorTexts` (`protocol.go`). At most 64 stderr records, 64 error texts, 8 panic and
  8 ONNX Runtime lines per run; then one line "search worker: further worker output dropped", and
  `Close` logs the counts. What the worker chooses (numbers, choices among fixed texts, lengths) is
  at most about 0.6 KB per run (about 72 bits per record × 64, 3 bits per text × 64, two counts). Not
  covered: the code of an error reply, and the exit status and timing of a failed process (bounded
  by the restart policy per 10 minutes, not per run). A new `eprintln!` or source file in
  `search/worker/src` needs its form or its entry in `workerSourceFiles`:
  `TestStderrFormsCoverWorker` reads the worker sources and fails until it is added.
- `make search-asan` runs the unsafe code (the zeroing allocator `search/zalloc`, the hardening and
  the sandbox of the worker, `-p worker --lib`) under AddressSanitizer
  (`RUSTFLAGS=-Zsanitizer=address`, `--target x86_64-unknown-linux-gnu`). It needs a nightly
  toolchain (`rustup toolchain install nightly`) and is not part of CI. Last run 2026-11-05 with
  nightly 1.101.0 (708c94ef0 2026-11-03): `zalloc` 3 + 4 tests, `worker` 126 tests, no sanitizer
  report.
- In CI (`CI=true`) on Linux with feature `test-probes`, `search/worker/build.rs` fails when `gdb`
  or `strace` is missing instead of marking their tests ignored: the core dump test and the system
  call test are evidence for the search spec (criterion 5). Locally a missing tool still only
  ignores its test.
