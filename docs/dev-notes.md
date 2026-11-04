# Development notes

Findings from the start of v2 work (plan task 1): how the Wails v3 skeleton is laid out
and what the three delivery-risk checks showed. Date: 2026-10-30, Ubuntu 26.04, amd64.

## Tools

| Tool | Version |
|---|---|
| Go | go1.27.3 (`go.mod`: `go 1.26.0`, `toolchain go1.27.3`) |
| Wails CLI and module | v3.0.0 |
| Task | 3.54.0 |
| golangci-lint | 2.15.0 |
| Node.js / npm | 24.21.0 / 12.0.2 |
| GTK / WebKitGTK (dev packages) | 4.22.4 / 2.52.6 |

`go mod tidy` rewrites `go 1.26` to `go 1.26.0`, because Wails v3.0.0 itself requires
`go 1.26.0`; both mean the same language version.

## Layout

- `cmd/smartnote/main.go` is the entry point. `//go:embed` cannot reach a parent directory,
  so the built UI is embedded by package `frontend` (`frontend/embed.go`, `frontend.Dist`),
  and `main` serves `fs.Sub(frontend.Dist, "dist")`.
- `frontend/dist` must exist before Go code that imports `frontend` compiles; `make test`,
  `make lint` and `make integration` build the frontend first (`task common:build:frontend`).
- The `Makefile` pins golangci-lint in `bin/` and runs `govulncheck` as a `tool` in `go.mod`;
  `.golangci.yml` lints the integration tests with the `integration` tag.
- `go.mod` has `ignore ./frontend/node_modules`, so `./...` never walks npm packages
  (some ship `.go` files).
- The root `Taskfile.yml` defines `MAIN_PKG: ./cmd/smartnote`; the platform Taskfiles under
  `build/` pass it to `go build` and to `wails3 generate bindings`.
- Package metadata (name, vendor, version, copyright, description) lives in `build/config.yml`;
  `wails3 task common:update:build-assets` copies it into `build/darwin/*.plist`,
  `build/windows/info.json`, `build/windows/nsis/wails_tools.nsh`, `build/linux/desktop` and
  `build/linux/nfpm/nfpm.yaml`. That task rewrites `nfpm.yaml` from the Wails template, which
  resets `maintainer` (to `${GIT_COMMITTER_NAME} <${GIT_COMMITTER_EMAIL}>`, empty in CI) and
  `homepage` (to `https://wails.io`): set them back after running it. `build/windows/msix/*.xml`
  is not generated and is kept by hand. The task also creates `build/ios/`, which the project does
  not use and does not keep.
- The icon source is `build/appicon.svg`; `build/appicon.png` is its 1024×1024 rendering, and
  `common:generate:icons` builds `windows/icon.ico` and `darwin/icons.icns` from that PNG.
  There is no Icon Composer `Assets.car`: on macOS with actool 26 Wails would rebuild it, and
  `icons.icns` with it, from `appicon.icon`, so macOS uses `icons.icns` (`CFBundleIconFile`).
- The template's server-mode tasks (`build:server`, `run:server`, `build:docker`, `run:docker`)
  and `build/docker/Dockerfile.server` are removed: Wails server mode serves the bound services
  over HTTP with no authentication or Origin check, and the spec has no such mode. The unused
  `build/linux/appimage/build.sh` is removed too; `wails3 generate appimage` builds the AppImage.

## Check 1: bindings and errors

A temporary service with `Echo(s string, n int64) (string, error)` and `Fail() error` was bound,
`wails3 generate bindings -ts -i ./cmd/smartnote` produced:

```ts
export function Echo(s: string, n: number): $CancellablePromise<string>
export function Fail(): $CancellablePromise<void>
```

The packaged app (AppImage in a clean container, see check 3) called both and reported:

```json
{"echoed":"ok:42","echoedType":"string","failThrew":true,"name":"RuntimeError","message":"probe failure text","cause":{}}
```

So `int64` is a TS `number`, `string` is `string`, and a returned Go error rejects the promise
with an `Error` whose `message` is `err.Error()`. The source of Wails v3.0.0
(`pkg/application/bindings.go`) shows the rejection is a `CallError{message, cause, kind}`:
`kind` is `RuntimeError`, and `cause` is `json.Marshal(&err)` (or the service's
`ServiceOptions.MarshalError`). A plain `errors.New` value marshals to `{}`; an error type
with exported JSON fields arrives in `cause` as that object. `wailsui` can therefore send
`{code, message, ...}` as the error value itself and the UI reads it from `e.cause`.

The temporary service was removed.

## Check 2: Content Security Policy

`frontend/index.html` carries the policy from the spec, section 7.3:

```
default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'
```

`base-uri 'none'` and `form-action 'none'` were added after the check below, following the code
review of tasks 1–3: the page has no `<base>` and no form that submits by navigation.

A temporary listener for `securitypolicyviolation` reported every violation to Go, together with
the computed colour of an element styled by a Svelte component `<style>` block. As a control,
the page added an `<img>` from `https://example.invalid/`, which must be reported.

| Mode | Violations | Component style |
|---|---|---|
| packaged AppImage (production build) | only the control (`img-src`) | applied |
| `wails3 dev` (Vite dev server) | `style-src-elem` for inline `<style>`, and the control | not applied |

Binding calls through the Wails runtime need nothing beyond the
policy. In dev mode Vite injects component CSS as inline `<style>` elements, so
`frontend/vite.config.ts` has a plugin that, for the dev server only (`apply: "serve"`), changes
`style-src 'self';` to `style-src 'self' 'unsafe-inline';` and fails if the strict string is
missing. After that change `wails3 dev` reports only the control, and `frontend/dist/index.html`
still contains `style-src 'self';`. The HMR WebSocket is allowed by `connect-src 'self'`.

`wails3 dev` needs a display, so it was run in an `ubuntu:26.04` container with the GTK and
WebKitGTK development packages and Xvfb; the probe was removed afterwards.

## Check 3: packages and the AppImage in a clean container

`task build` and `task package:linux` produce `bin/smartnote`, `bin/smartnote.deb` and
`bin/smartnote-x86_64.AppImage`. The `.deb` depends on `libgtk-4-1`, `libwebkitgtk-6.0-4`,
`fonts-liberation` (the interface font; `liberation-sans-fonts` in the `.rpm`, `ttf-liberation` for Arch) and
`fonts-dejavu-core` (DejaVu Sans Mono for the notes; `dejavu-sans-mono-fonts`, `ttf-dejavu`).

`task smoke:appimage` runs `build/linux/smoke-appimage.sh` in `ubuntu:26.04`. With only
`xvfb` and `xauth` installed, as first planned, the AppImage does not start:

1. `libharfbuzz.so.0: cannot open shared object file`. `ldd` over the extracted AppImage
   also lists `libEGL.so.1`, `libfontconfig.so.1`, `libfribidi.so.0`; `libGLESv2.so.2` is
   loaded with `dlopen` and fails later. These libraries are on the AppImage exclude list
   and are expected from the host.
2. `Failed to spawn child process "/usr/bin/bwrap"`, then SIGABRT: WebKitGTK 6.0 starts its
   web process in a bubblewrap sandbox and needs the host's `bwrap` (and `xdg-dbus-proxy`).

Every Ubuntu 26.04 desktop has these packages (`libwebkitgtk-6.0-4` depends on `bubblewrap`
and `xdg-dbus-proxy`; the libraries belong to the desktop base). They are not development
packages, so the check now installs them:
`xvfb xauth dbus libegl1 libgles2 libfontconfig1 libfribidi0 libharfbuzz0b bubblewrap xdg-dbus-proxy`.

bubblewrap needs an unprivileged user and user namespaces, which Docker blocks by default.
The container runs with `--security-opt seccomp=unconfined --security-opt apparmor=unconfined
--security-opt systempaths=unconfined`, and the app runs as a regular user under
`xvfb-run -a dbus-run-session`. As root inside the container bwrap still fails
(`Creating new namespace failed`).

Result: `smoke: exit code 0`, `smoke: ok`: the AppImage stays up for 15 seconds with a running
`WebKitWebProcess`, the log has no library or crash errors, and SIGTERM ends it with code 0.
A control run without `bubblewrap` fails the check (`application exited early`, exit code 2).

Consequence for README (task 15): the AppImage supports Ubuntu desktops; a minimal system
needs the packages above.

## Packages with search (plan task 16)

Every package ships the search worker `smartnote-search`, the official ONNX Runtime 1.31.0
library and the model files `model.onnx`, `tokenizer.kit` and `manifest.json` at the paths of the
specification, section 8.2 (`tokenizer.json` is only for the tests and is not shipped), and the
licence notices: `THIRD_PARTY_NOTICES.md` with `licenses/onnxruntime/{LICENSE,ThirdPartyNotices.txt}`,
`licenses/rust-crates.txt` and `licenses/go-modules.txt`.

- `task search:files` prepares them and is a dependency of `linux:create:appimage`,
  `linux:create:deb` (and `create:rpm`, `create:aur`), `darwin:create:app:bundle` and
  `windows:package`, so `task package:<os>` works from a clean checkout. It runs
  `search:build`, `model:fetch` and `ort:fetch`; the package tasks run `notices:build` themselves
  once the application is built, because `go list` needs `frontend/dist` (`go:embed`). Each of
  them runs once per `task` call.
- `task search:build` runs `tools/searchbuild`: `cargo build --locked --release -p worker` for
  the Rust target of `GOOS`/`ARCH` (`linux/amd64` → `x86_64-unknown-linux-gnu`, `darwin/arm64` →
  `aarch64-apple-darwin`, `darwin/amd64` → `x86_64-apple-darwin`, `windows/amd64` →
  `x86_64-pc-windows-msvc`) and copies the executable named in cargo's JSON messages to
  `bin/smartnote-search[.exe]`, so `CARGO_TARGET_DIR` and `RUSTUP_TOOLCHAIN` of the environment
  are honoured. Cargo runs every time. A `GOOS` other than the host's, or a target without a
  worker, stops with a message: the worker is not cross-compiled.
- `task ort:fetch` also extracts `LICENSE` and `ThirdPartyNotices.txt` from the release archive
  (the `notices` of `ort.lock.json`; the archive SHA-256 covers them) into `build/ort`.
- `task notices:build` runs `tools/notices`: the crates the worker links for its target, from
  `cargo metadata` (normal dependencies of package `worker`, without dev and build ones), and the
  Go modules of `go list -deps -tags production ./cmd/smartnote` for the target system, each with
  the licence files of its source package (`LICENSE*`, `COPYING*`, `NOTICE*` and the like;
  identical texts are written once). Crates without licence files: the tantivy crates are covered
  by the files of `tantivy` (same repository); `kitoken` by the BSD-2-Clause text of its repository
  at v0.11.1, kept in `tools/notices/extra/kitoken/LICENSE`; `htmlescape` 0.3.1 (Apache-2.0 / MIT
  / MPL-2.0) has none anywhere, the list names its licence and the tool warns about it.
- `.deb`: `contents` of `nfpm.yaml`, worker 0755, other files 0644; the notices go to
  `/usr/share/doc/smartnote/`. `task package:linux` ends with `task check:deb`
  (`build/linux/check-deb.sh`, from `dpkg-deb -c`).
- AppImage: `wails3 generate appimage` puts only the application into the AppDir and runs
  linuxdeploy on it. Running linuxdeploy again after adding files would deploy them too and set
  the RPATH of the ONNX Runtime library, which changes the file and breaks its pinned SHA-256. So
  the files are added to the AppDir that Wails leaves in `build/linux/appimage/build`, and the AppDir
  is packed again by `linuxdeploy-plugin-appimage` from the same linuxdeploy AppImage (extracted
  with `--appimage-extract`), which only packs. The AppRun of Wails is kept.
- macOS: the files are copied into the bundle before `codesign:adhoc`. The ONNX Runtime dylib of
  Microsoft is signed (Developer ID, a CMS blob in its signature); `codesign --force --deep` on the
  bundle would sign it again and change the file, and the worker would refuse it. So the worker is
  signed on its own, the bundle without `--deep`, `codesign --verify --deep --strict` checks all of
  it, and `cmp` checks that the library in the bundle is the fetched one.
- Windows: `project.nsi` installs the files beside `smartnote.exe` (`model\`, `licenses\`) and the
  uninstaller deletes them before removing the folder.

`task smoke:appimage` builds `cmd/searchsmoke` statically (`CGO_ENABLED=0`) into `build/smoke`;
after the GUI check `build/linux/smoke-appimage.sh` extracts the AppImage as the unprivileged user
and runs it with the worker, the library and the model of the extracted image, from `usr/lib` and
with the `LD_LIBRARY_PATH` that AppRun gives the application (the adapter passes it on to the
worker). Result in `ubuntu:26.04`: `ready worker=0.1.0 lexical=ready semantic=ready sandbox=full`,
`hits=1`. The AppImage bundles no `libstdc++`; ONNX Runtime loads the system's one under the
worker's sandbox. A run with the AppImage of v2.0.1 fails the check (`no such file or directory`
for the worker).

Package sizes (2026-11-04): `.deb` 8.8 MB → 134.8 MB, AppImage 135.9 MB → 254.5 MB.

CI job `package` installs the toolchain of `search/rust-toolchain.toml`, caches cargo
(`Swatinem/rust-cache`), `build/model` and `build/ort` with the keys of job `search`, then runs
`task package:<os>`. On macOS and Windows it runs `cmd/searchsmoke` against the packaged worker
(the signed bundle on macOS) with `ORT_DISABLE_TELEMETRY=1`. The model release `model-e5s-1` must be
published before `task model:fetch` can work in CI; locally `MODEL_BASE_URL=file:///…/` points it
at a directory.
