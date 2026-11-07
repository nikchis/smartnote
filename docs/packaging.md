**English** | [Русский](packaging.ru.md)

# Packaging

What goes into the Linux, Windows and macOS packages and how the package tasks build them: the
metadata and the icon, the search worker with its model and ONNX Runtime library, the licence
notices and signing on macOS. For developers who build or change the packages; the commands are in
[Building from source](building.md).

## Metadata and icon

- Package metadata (name, vendor, version, copyright, description) lives in `build/config.yml`;
  `wails3 task common:update:build-assets` copies it into `build/darwin/*.plist`,
  `build/windows/info.json`, `build/linux/desktop` and
  `build/linux/nfpm/nfpm.yaml`. That task rewrites `nfpm.yaml` from the Wails template, which
  resets `maintainer` (to `${GIT_COMMITTER_NAME} <${GIT_COMMITTER_EMAIL}>`, empty in CI) and
  `homepage` (to `https://wails.io`): set them back after running it. `build/windows/msix/*.xml`
  is not generated and is kept by hand. The task also creates `build/ios/`, which the project does
  not use and does not keep, and `build/windows/nsis/`, which it no longer uses since the MSI
  package: delete both.
- The icon source is `build/appicon.svg`; `build/appicon.png` is its 1024×1024 rendering, and
  `common:generate:icons` builds `windows/icon.ico` and `darwin/icons.icns` from that PNG.
  There is no Icon Composer `Assets.car`: on macOS with actool 26 Wails would rebuild it, and
  `icons.icns` with it, from `appicon.icon`, so macOS uses `icons.icns` (`CFBundleIconFile`).
- Linux desktop integration is named after the application id `com.nikchis.smartnote`
  (`linuxAppID` in `cmd/smartnote`): the desktop file `com.nikchis.smartnote.desktop` in the `.deb`
  and in the AppImage, so Wayland desktops match the window with it, and the AppStream metadata
  `build/linux/com.nikchis.smartnote.metainfo.xml` (by hand; add a `<release>` for every version).
  The `.deb` installs it in `/usr/share/metainfo/`; the AppImage carries it as
  `usr/share/metainfo/com.nikchis.smartnote.appdata.xml`, the name appimagetool looks for, and
  appimagetool fails the build when `appstreamcli validate` finds even a warning. Check a change
  with `appstreamcli validate --no-net build/linux/com.nikchis.smartnote.metainfo.xml`.

## Packages with search

Since release 2.1.0 (plan task 16) every package ships the search worker `smartnote-search`, the
official ONNX Runtime 1.31.0 library and the model files `model.onnx`, `tokenizer.kit` and
`manifest.json` at the paths of the specification, section 8.2 (`tokenizer.json` is only for the
tests and is not shipped), and the licence notices: `THIRD_PARTY_NOTICES.md` with `licenses/onnxruntime/{LICENSE,ThirdPartyNotices.txt}`,
`licenses/rust-crates.txt` and `licenses/go-modules.txt`.

- `task search:files` prepares them and is a dependency of `linux:create:appimage`,
  `linux:create:deb` (and `create:rpm`, `create:aur`), `darwin:create:app:bundle` and
  `windows:create:msi` (the package of `windows:package`), so `task package:<os>`
  works from a clean checkout. It runs
  `search:build`, `model:fetch` and `ort:fetch`; the package tasks run `notices:build` themselves
  once the application is built, because `go list` needs `frontend/dist` (`go:embed`). Each of
  them runs once per `task` call.
- `task search:build` runs `tools/searchbuild`: `cargo build --locked --release -p worker` for
  the Rust target of `GOOS`/`ARCH` (`linux/amd64` → `x86_64-unknown-linux-gnu`, `darwin/arm64` →
  `aarch64-apple-darwin`, `darwin/amd64` → `x86_64-apple-darwin`, `windows/amd64` →
  `x86_64-pc-windows-msvc`) and copies the executable named in cargo's JSON messages to
  `bin/smartnote-search[.exe]`, so `CARGO_TARGET_DIR` and `RUSTUP_TOOLCHAIN` of the environment
  are honoured. Cargo runs every time. A `GOOS` other than the host's, or a target without a
  worker, stops with a message: the worker is not cross-compiled. On Windows the worker links the
  C runtime statically (`search/.cargo/config.toml`, `-C target-feature=+crt-static`; a
  `RUSTFLAGS` variable would replace it): the package ships no Visual C++ Redistributable. The
  official `onnxruntime.dll` still imports `MSVCP140.dll`, `MSVCP140_1.dll`, `VCRUNTIME140.dll`
  and `VCRUNTIME140_1.dll`; without the Redistributable it does not load, semantic search is
  unavailable ("semantic search unavailable: loading the ONNX Runtime library failed" in the
  log) and full-text search works (`tests/e2e.rs`, `ort_load_failure_keeps_full_text_search`,
  checks this with a broken dependency of the library on Linux). These DLLs are not copied
  into the package: they would be unpinned binaries.
- `task ort:fetch` fetches the library of `GOOS`/`ARCH` and also extracts `LICENSE` and
  `ThirdPartyNotices.txt` from the release archive (the `notices` of `ort.lock.json`; the archive
  SHA-256 covers them, a later run only checks that they are present) into `build/ort`. A target
  without a pinned library (darwin/amd64, linux/arm64) stops here with a message; so do
  `darwin:package:universal` (the worker and the library are arm64 only) and the MSIX package
  (`FORMAT=msix`, which would not contain the search files).
- `task notices:build` runs `tools/notices`: the crates the worker links for its target, from
  `cargo metadata` (normal dependencies of package `worker`, without dev and build ones), and the
  Go modules of `go list -deps -tags production ./cmd/smartnote` for the target system, each with
  the licence files of its source package (`LICENSE*`, `COPYING*`, `NOTICE*` and the like;
  identical texts are written once). Crates without licence files: the tantivy crates are covered
  by the files of `tantivy` (same repository); `kitoken` by the BSD-2-Clause text of its repository
  at v0.11.1, kept in `tools/notices/extra/kitoken/LICENSE`; `htmlescape` 0.3.1 (Apache-2.0 / MIT
  / MPL-2.0) has none in the crate or its repository, so `extra/htmlescape/LICENSE-MIT` is the MIT
  License with the author of its `Cargo.toml` as the copyright holder. The Rust standard library is
  listed with the version of `rustc` and the `LICENSE-MIT` and `LICENSE-APACHE` of rust-lang/rust
  at 1.99.0 (`extra/rust-std`; the copies under the toolchain's `share/doc/rust/licenses` are
  templates without the copyright line), the Go standard library with `$GOROOT/LICENSE`. A package
  with no licence text at all is named in a warning.
- `.deb`: `contents` of `nfpm.yaml`, worker 0755, other files 0644; the notices go to
  `/usr/share/doc/smartnote/`. `task package:linux` ends with `task check:deb`
  (`build/linux/check-deb.sh`, from `dpkg-deb -c`).
- AppImage: `wails3 generate appimage` puts only the application into the AppDir and runs
  linuxdeploy on it. Running linuxdeploy again after adding files would deploy them too and set
  the RPATH of the ONNX Runtime library, which changes the file and breaks its pinned SHA-256. So
  the files are added to the AppDir that Wails leaves in `build/linux/appimage/build`, and the AppDir
  is packed again by `linuxdeploy-plugin-appimage` from the same linuxdeploy AppImage (extracted
  with `--appimage-extract`), which only packs. The AppRun of Wails is kept.
- Windows: `build/windows/wix/smartnote.wxs` describes a per-user MSI (WiX Toolset v6) that puts
  the files into `%LOCALAPPDATA%\Programs\Smartnote` (`model\`, `licenses\` beside
  `smartnote.exe`) without administrator rights, takes its version from the version resource of
  `smartnote.exe` (`build/windows/info.json`) and replaces an older version (`MajorUpgrade`, fixed
  `UpgradeCode`). Every component lives in the user profile, so its key path is a value under
  `HKCU\Software\nikchis\Smartnote` and each folder has a `RemoveFolder`; `wix msi validate` runs
  the ICE checks. A new file in the package needs a `File` in one of its components. The package
  has no custom actions and no dialogs of its own and does not install the WebView2 Runtime: a
  launch condition refuses the installation without it (the `pv` value in `HKLM` or `HKCU`).
  The Go application for Windows is linked without `-s -w`, with its symbol tables. Both choices
  aim at the machine-learning detection `Trojan:Win32/Wacatac` of Microsoft Defender, which
  blocked the download of the unsigned NSIS installer of 2.2.0: stripped Go executables and NSIS
  installers are reported to trigger it, though neither is a proven cause.

The model files are prepared and published as [tools/model/README.md](../tools/model/README.md)
describes; the notices file is [THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md).

## macOS bundle and signing

The files are copied into the bundle before `codesign:adhoc`. The ONNX Runtime dylib of
Microsoft is signed (Developer ID, a CMS blob in its signature); `codesign --force --deep` on the
bundle would sign it again and change the file, and the worker would refuse it. So the worker is
signed on its own, the bundle without `--deep`, `codesign --verify --deep --strict` checks all of
it, and `cmp` checks that the library in the bundle is the fetched one.

Trap: `task darwin:sign` and `darwin:sign:notarize` run `wails3 tool sign`, which signs with
`codesign --deep --force`: it would sign the dylib again and silently turn semantic search off.
Signing with a Developer ID needs the same order as `codesign:adhoc`, and the hardened runtime
that notarization requires enforces library validation: the worker loading a library signed by
another team (Microsoft) then needs a decision, e.g. the entitlement
`com.apple.security.cs.disable-library-validation` for the worker, or a copy signed with our own
identity, whose SHA-256 would have to be pinned instead.

## AppImage smoke check

`task smoke:appimage` builds `cmd/searchsmoke` statically (`CGO_ENABLED=0`) into `build/smoke`;
after the GUI check ([development notes, check 3](development.md#check-3-packages-and-the-appimage-in-a-clean-container))
`build/linux/smoke-appimage.sh` extracts the AppImage as the unprivileged user
and runs it with the worker, the library and the model of the extracted image, from `usr/lib` and
with the `LD_LIBRARY_PATH` that AppRun gives the application (the adapter passes it on to the
worker). Result in `ubuntu:26.04`: `ready worker=0.1.0 lexical=ready semantic=ready sandbox=full`,
`hits=1`. The AppImage bundles no `libstdc++`; ONNX Runtime loads the system's one under the
worker's sandbox. A run with the AppImage of v2.0.1 fails the check (`no such file or directory`
for the worker).

## Sizes and CI

Package sizes without and with the search files (2026-11-04): `.deb` 8.8 MB → 134.8 MB, AppImage 135.9 MB → 254.5 MB.

CI job `package` installs the toolchain of `search/rust-toolchain.toml`, caches cargo
(`Swatinem/rust-cache`), `build/model` and `build/ort` with the keys of job `search`, then runs
`task package:<os>`. On macOS and Windows it runs `cmd/searchsmoke` against the packaged worker
(the signed bundle on macOS) with `ORT_DISABLE_TELEMETRY=1`. On Windows it first installs the MSI
with `msiexec /qn`, checks the installed files, the Start menu shortcut and the entry for
uninstalling, runs `searchsmoke` against the installed files, then uninstalls the package and
checks that the folder is gone. The model release `model-e5s-1` must be
published before `task model:fetch` can work in CI; locally `MODEL_BASE_URL=file:///…/` points it
at a directory.
