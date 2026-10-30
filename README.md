## Smartnote

A notepad for private records. Notes are kept in an encrypted local storage;
optionally they are backed up to and restored from your own PostgreSQL server
reachable over SSH.

Version 2 is a rewrite of the Delphi application (branch `1.0.0`) in Go,
Wails v3 and Svelte. It is under development; installation, server setup and
the threat model will be described here before the first release.

### Building from source

Tools:

| Tool | Version |
|---|---|
| Go | 1.27.3 (`toolchain` in `go.mod`) |
| Node.js | LTS (developed with 24.21.0) |
| Wails CLI | v3.0.0: `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0` |
| Task | 3.54.0: `go install github.com/go-task/task/v3/cmd/task@v3.54.0` |
| golangci-lint | 2.15.0: `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0` |

On Ubuntu 26.04 the build also needs `build-essential pkg-config libgtk-4-dev
libwebkitgtk-6.0-dev` (`wails3 doctor` lists what is missing).

Tasks:

| Command | Purpose |
|---|---|
| `task dev` | run with live reload |
| `task build` | build `bin/smartnote` |
| `task test` | Go unit tests with the race detector |
| `task lint` | golangci-lint |
| `task package:linux` | `.deb` and AppImage in `bin/` |
| `task smoke:appimage` | start the AppImage in a clean `ubuntu:26.04` container (needs Docker) |

### License

MIT, see [LICENSE](LICENSE).
