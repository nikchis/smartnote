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
- `frontend/dist` must exist before Go code that imports `frontend` compiles; `task test`
  and `task lint` build the frontend first.
- `go.mod` has `ignore ./frontend/node_modules`, so `./...` never walks npm packages
  (some ship `.go` files).
- The root `Taskfile.yml` defines `MAIN_PKG: ./cmd/smartnote`; the platform Taskfiles under
  `build/` pass it to `go build` and to `wails3 generate bindings`.
- Package metadata in `build/config.yml`, `build/linux/nfpm/nfpm.yaml`, `build/darwin/*.plist`
  and `build/windows/*` is still the template's ("My Company"); it is filled in before release.
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
`bin/smartnote-x86_64.AppImage`. The `.deb` depends on `libgtk-4-1` and `libwebkitgtk-6.0-4`.

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
