**English** | [Русский](install.ru.md)

# Installation and upgrade

How to install Smartnote on Linux, Windows and macOS, what semantic search needs from the system,
how to upgrade from 2.2.x, 2.1.0 and 2.0.x and where the application keeps its files. For users.

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

Windows 10/11, file `Smartnote-<version>-windows-amd64.msi`. SmartScreen shows "Windows protected
your PC": choose **More info**, then **Run anyway**.

The package installs Smartnote for the current user into `%LOCALAPPDATA%\Programs\Smartnote`,
without administrator rights, and adds shortcuts to the Start menu and the desktop. It needs the
Microsoft Edge WebView2 Runtime, which Windows 11 and an updated Windows 10 have; without it the
package refuses to install and names the page to download the Runtime from. A newer package
replaces the installed version. Smartnote is removed in **Settings → Apps**; the notes
(`%APPDATA%\smartnote`) and the data of WebView2 (`%APPDATA%\smartnote.exe`) stay.

Semantic search needs the Microsoft Visual C++ Redistributable (x64), which most systems have;
without it only full-text search works.

## macOS

macOS 13+ on Apple Silicon (arm64), file `Smartnote-<version>-macos-arm64.dmg`. There is no package for Intel Macs: the
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

## Upgrading from 2.2.x

2.3.0 keeps the local storage in format 2, so the upgrade needs no action: the notes, the settings
and the Yandex Disk sign-in stay. 2.3.0 adds [Google Drive](google-drive.md) as a storage, the
interface in Russian and the privacy policy in the application (see
[Desktop integration](#desktop-integration)). On the first unlock it adds two tables to `local.db`,
for the Google sign-in and for an unfinished upload to Google Drive; 2.2.x does not read them.

Going back to 2.2.x keeps `local.db` readable, with the notes and the Yandex Disk sign-in. 2.2.x does
not know Google Drive: if it is chosen, Connect fails, and the Settings of 2.2.x let you choose
another storage. Saving the Settings in 2.2.x drops your own Google client ID and client secret.
After returning to 2.3.0 enter the same pair again: the saved Google sign-in then works again,
unless Google has revoked it, and a new sign-in is needed only with another client. 2.2.x ignores
`prefs.json` and is always in English.

## Upgrading from 2.1.0

2.2.0 keeps the local storage in format 2, so the upgrade needs no action. The server settings
stay; **Settings** shows them under **SSH + PostgreSQL**. Going back to 2.1.0 keeps `local.db`
readable, but settings for Yandex Disk or **None (local only)** may need to be cleared in the
Settings of 2.1.0: see [Limitations](limitations.md).

## Upgrading from 2.0.x

2.1.0 adds local search and keeps the local storage in format 2, which holds the encrypted search
data of the notes. The first unlock with 2.1.0 or later converts `local.db` for good, and 2.0.x
then refuses it with "Data was created by a newer version of Smartnote.". There is no conversion
back.

To return to 2.0.x:

1. Back up the notes to your server with 2.1.0 (**Save**) or later (**Backup**).
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

The language of the interface is kept in `prefs.json` next to `local.db`, in plain text: it is
needed before the unlock and is not secret. Until you switch the language, Smartnote follows the
language of the system (Russian for a Russian system, English otherwise) and writes no such file.

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
- The interface is in English or Russian. The **EN**/**RU** button right of the search bar on the
  Main screen shows the current language and switches to the other one: screens, dialogs, tooltips,
  messages, the progress of transfers, the browser pages of a sign-in and the titles of file
  dialogs. Technical texts of SSH and PostgreSQL errors stay as the libraries give them.
- The shield button, the last one on the right, opens the privacy policy in the language of the
  interface. It is a copy kept in the application, so it opens without the network; its links open
  in the browser.

Next step, if the notes should be backed up: [Server setup](server-setup.md) for your own server,
[Yandex Disk](yandex-disk.md) or [Google Drive](google-drive.md).
