**English** | [Русский](install.ru.md)

# Installation and upgrade

How to install Smartnote on Linux, Windows and macOS, what semantic search needs from the system,
how to upgrade from 2.0.x and where the application keeps its files. For users.

The packages are on the [releases page](https://github.com/nikchis/smartnote/releases). They are not
signed, so Windows and macOS warn before the first start.

## Ubuntu (.deb)

Ubuntu 26.04, amd64, file `smartnote_<version>_amd64.deb`:

```
sudo apt install ./smartnote_<version>_amd64.deb
```

It pulls in GTK 4 and WebKitGTK 6.0.

## Ubuntu (AppImage)

Ubuntu desktop, amd64, file `Smartnote-<version>-x86_64.AppImage`: make it executable (`chmod +x`) and run
it. A minimal system also needs `libegl1 libgles2 libfontconfig1 libfribidi0 libharfbuzz0b
bubblewrap xdg-dbus-proxy`, which every Ubuntu desktop has.

## Windows

Windows 10/11, file `Smartnote-<version>-windows-amd64-installer.exe`. SmartScreen shows "Windows protected your PC":
choose **More info**, then **Run anyway**.

Semantic search needs the Microsoft Visual C++ Redistributable (x64), which most systems have;
without it only full-text search works.

## macOS

macOS 12+ on Apple Silicon (arm64), file `Smartnote-<version>-macos-arm64.dmg`. There is no package for Intel Macs: the
ONNX Runtime library of the search is pinned for Apple Silicon only ([Packaging](packaging.md)).

Gatekeeper refuses an unsigned app: drag Smartnote to Applications, then either open
**System Settings → Privacy & Security** and press **Open Anyway** after the first attempt, or run

```
xattr -dr com.apple.quarantine /Applications/smartnote.app
```

## Semantic search requirements

On x86-64 computers semantic search needs a CPU with AVX2; on Windows also the Visual C++
Redistributable (see above). Without them Smartnote works, and full-text search too: the status
line of the search says `Semantic search unavailable` ([Search](search.md#status-line)).

## Upgrading from 2.0.x

2.1.0 adds local search and keeps the local storage in format 2, which holds the encrypted search
data of the notes. The first unlock with 2.1.0 converts `local.db` for good, and 2.0.x then refuses
it with "Data was created by a newer version of Smartnote.". There is no conversion back.

To return to 2.0.x:

1. Back up the notes to your server with 2.1.0 (**Save**).
2. In 2.0.x start with a new local storage (move `local.db` away).
3. Set up the connection in **Settings** again. The SSH key is kept in `local.db` as well: import
   your key file, or generate a new key and authorize it on the server.
4. **Restore**.

## Where the data lives

| System | Local storage (`local.db`) | Error log (`smartnote.log`) |
|---|---|---|
| Linux | `$XDG_DATA_HOME/smartnote` (`~/.local/share/smartnote`) | `$XDG_STATE_HOME/smartnote` (`~/.local/state/smartnote`) |
| Windows | `%APPDATA%\smartnote` | `%LOCALAPPDATA%\smartnote` |
| macOS | `~/Library/Application Support/smartnote` | `~/Library/Logs/smartnote` |

The log holds errors only, without passwords, the sync phrase, keys, note names and texts or the
connection string.

On Linux the logo of the title bar is written to `$XDG_CACHE_HOME/smartnote/icons`
(`~/.cache/smartnote/icons`), because GTK 4 takes window icons from an icon theme only; it holds
nothing else and is written again if deleted.

## Desktop integration

- Only one Smartnote runs at a time: a second start brings the open window forward.
- The application id is `com.nikchis.smartnote`, and the `.deb`, the `.rpm` and the AppImage carry
  the desktop file `com.nikchis.smartnote.desktop` under that name, so Wayland desktops show the
  logo for the window, and AppStream metadata for software centres.
- The interface uses Liberation Sans (`fonts-liberation`), the notes DejaVu Sans Mono 13pt
  (`fonts-dejavu-core`); both are dependencies of the packages.
- The colors are the dark skin of v1, whatever theme the system prefers.

Next step: [Server setup](server-setup.md), if the notes should be backed up to your server.
