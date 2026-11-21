**English** | [Русский](building.ru.md)

# Building from source

The tools, system packages and commands needed to build, test and package Smartnote, for
developers.

## Tools

| Tool | Version |
|---|---|
| Go | 1.27.4 (`toolchain` in `go.mod`) |
| Node.js / npm | LTS (developed with 24.21.0 / 12.0.2) |
| Rust | 1.99.0 with rustfmt and clippy (`search/rust-toolchain.toml`) |
| Wails CLI | v3.1.0: `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0` |
| Task | 3.54.0: `go install github.com/go-task/task/v3/cmd/task@v3.54.0` |
| golangci-lint | 2.15.0: `make install-lint` puts it in `bin/` |

`go.mod` says `go 1.26.0`, not `go 1.26`: `go mod tidy` rewrites it so, because Wails v3 itself
requires `go 1.26.0`; both mean the same language version.

On Ubuntu 26.04 the build also needs `build-essential pkg-config libgtk-4-dev
libwebkitgtk-6.0-dev` (`wails3 doctor` lists what is missing; developed with GTK 4.22.4 and
WebKitGTK 2.52.6). The Windows package needs the WiX Toolset v6 (`dotnet tool install --global wix
--version 6.0.2`).

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
| `task package:windows` | per-user MSI package in `bin/` (on Windows) |
| `task package:darwin` | `.app` and `.dmg` in `bin/` (on macOS) |
| `task smoke:appimage` | start the AppImage in a clean `ubuntu:26.04` container (needs Docker) |

The manual checks of the packages are in [Manual acceptance test](manual-test.md) and
[Smoke test on Windows and macOS](smoke-windows-macos.md).

## Built-in Google client

The client ID and the client secret of the Desktop OAuth client with which
[Google Drive](google-drive.md) signs in with one click are not in the repository. The build puts
them into the binary from two environment variables:

```sh
export SMARTNOTE_GOOGLE_CLIENT_ID=...       # <digits>-<...>.apps.googleusercontent.com
export SMARTNOTE_GOOGLE_CLIENT_SECRET=...
make build                                  # or task build, task package:linux and so on
```

The values come from the owner of the project. Every build and package task (`task build`,
`task dev`, `task package:*`, including the cross builds in Docker once `wails3 task setup:docker` has rebuilt
the image) passes them to the linker as
`-ldflags "-X github.com/nikchis/smartnote/internal/adapter/gdrive.BuiltinClientID=...
-X github.com/nikchis/smartnote/internal/adapter/gdrive.BuiltinClientSecret=..."`; the command
line that Task prints shows the names of the variables, not the values. The values must not
contain spaces. Do not write them into the files of the repository.

Without both variables the build still succeeds, but has no built-in Google client: Google Drive
then works only with your own client in **Settings → Advanced** ([Your own client](google-drive.md#your-own-client)),
and **Sign in with Google** without it says so. Yandex Disk is not affected. CI takes the values
from the repository secrets `SMARTNOTE_GOOGLE_CLIENT_ID` and `SMARTNOTE_GOOGLE_CLIENT_SECRET`
(**Settings → Secrets and variables → Actions** on GitHub) for the `package` job. To check the
values a build received without printing them, run
`go test ./internal/adapter/gdrive -run TestBuiltinClientOfBuild -ldflags "<the same -X flags>"`.
