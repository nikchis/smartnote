**English** | [Русский](building.ru.md)

# Building from source

The tools, system packages and commands needed to build, test and package Smartnote, for
developers.

## Tools

| Tool | Version |
|---|---|
| Go | 1.27.3 (`toolchain` in `go.mod`) |
| Node.js / npm | LTS (developed with 24.21.0 / 12.0.2) |
| Rust | 1.99.0 with rustfmt and clippy (`search/rust-toolchain.toml`) |
| Wails CLI | v3.1.0: `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0` |
| Task | 3.54.0: `go install github.com/go-task/task/v3/cmd/task@v3.54.0` |
| golangci-lint | 2.15.0: `make install-lint` puts it in `bin/` |

`go.mod` says `go 1.26.0`, not `go 1.26`: `go mod tidy` rewrites it so, because Wails v3 itself
requires `go 1.26.0`; both mean the same language version.

On Ubuntu 26.04 the build also needs `build-essential pkg-config libgtk-4-dev
libwebkitgtk-6.0-dev` (`wails3 doctor` lists what is missing; developed with GTK 4.22.4 and
WebKitGTK 2.52.6). The Windows installer needs NSIS (`makensis`).

Some commands need more: Docker for `make integration` and `task smoke:appimage`; `gdb` and
`strace` for `make search-test`; `cargo-deny` for `make search-lint`; a nightly Rust toolchain for
`make search-asan`; [uv](https://docs.astral.sh/uv/) for `task model:build`.

## Commands

Day-to-day checks run through `make` (`make help` lists the targets); Wails builds and
packages run through Task, which `make build`, `make package` and `make smoke` call.

| Command | Purpose |
|---|---|
| `make all` | `dep`, `test` and `lint` |
| `make test` | Go unit tests with the race detector and total coverage |
| `make integration` | integration tests against PostgreSQL and OpenSSH in Docker |
| `make lint` | golangci-lint (installed to `bin/` at the pinned version) and govulncheck |
| `make fmt`, `make lint-fix` | format the code, apply automatic lint fixes |
| `make search-test` | tests of the Rust search worker with the probe binaries; `SEARCH_MODEL=1` adds the model tests |
| `make search-lint` | rustfmt, clippy and the `cargo-deny` policy of the search worker |
| `make search-bench` | search quality and performance on the benchmark set (needs the model; takes minutes) |
| `make search-asan` | the unsafe code of the worker under AddressSanitizer ([Search maintenance](search-maintenance.md)) |
| `npm --prefix frontend run test` | interface unit tests (Vitest) |
| `npm --prefix frontend run check`, `npm --prefix frontend run lint` | svelte-check and ESLint |
| `task dev` | run with live reload |
| `task build` | build `bin/smartnote` |
| `task search:files` | build the search worker and fetch the model and the ONNX Runtime library ([Packaging](packaging.md)) |
| `task package:linux` | `.deb` and AppImage in `bin/` |
| `task package:windows` | NSIS installer in `bin/` (on Windows) |
| `task package:darwin` | `.app` and `.dmg` in `bin/` (on macOS) |
| `task smoke:appimage` | start the AppImage in a clean `ubuntu:26.04` container (needs Docker) |

The manual checks of the packages are in [Manual acceptance test](manual-test.md) and
[Smoke test on Windows and macOS](smoke-windows-macos.md).
