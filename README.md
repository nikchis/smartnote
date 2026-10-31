## Smartnote

A notepad for private records. Notes are kept in an encrypted local storage;
optionally they are backed up to and restored from your own PostgreSQL server
reachable over SSH.

Version 2 is a rewrite of the Delphi application (branch `1.0.0`) in Go,
Wails v3 and Svelte. It runs on Linux, Windows and macOS.

### Installation

Packages are not signed.

| System | Package | Notes |
|---|---|---|
| Ubuntu 26.04 (amd64) | `smartnote.deb` | `sudo apt install ./smartnote.deb` pulls in GTK 4 and WebKitGTK 6.0. |
| Ubuntu desktop (amd64) | `smartnote-x86_64.AppImage` | `chmod +x` and run. A minimal system also needs `libegl1 libgles2 libfontconfig1 libfribidi0 libharfbuzz0b bubblewrap xdg-dbus-proxy`, which every Ubuntu desktop has. |
| Windows 10/11 | `smartnote-amd64-installer.exe` | SmartScreen shows "Windows protected your PC": choose **More info**, then **Run anyway**. |
| macOS | `smartnote.dmg` | Gatekeeper refuses an unsigned app: drag Smartnote to Applications, then either open **System Settings → Privacy & Security** and press **Open Anyway** after the first attempt, or run `xattr -dr com.apple.quarantine /Applications/smartnote.app`. |

Where the data lives:

| System | Local storage (`local.db`) | Error log (`smartnote.log`) |
|---|---|---|
| Linux | `$XDG_DATA_HOME/smartnote` (`~/.local/share/smartnote`) | `$XDG_STATE_HOME/smartnote` (`~/.local/state/smartnote`) |
| Windows | `%APPDATA%\smartnote` | `%LOCALAPPDATA%\smartnote` |
| macOS | `~/Library/Application Support/smartnote` | `~/Library/Logs/smartnote` |

The log holds errors only, without passwords, the sync phrase, keys, note names and texts or the
connection string. Only one Smartnote runs at a time: a second start brings the open window forward.

### Server setup

Synchronization is optional: with empty settings Smartnote works locally only. The server needs
OpenSSH and PostgreSQL; Smartnote connects over SSH and reaches PostgreSQL through the SSH
connection, so the database never has to listen on a public address.

1. PostgreSQL listens on loopback only (`postgresql.conf`):

   ```
   listen_addresses = 'localhost'
   ```

   and `pg_hba.conf` lets the user in from loopback with a password, e.g.
   `host smartnote smartnote 127.0.0.1/32 scram-sha-256`.

2. Create the user and the database, and either create the schema for the user or let the
   application create it:

   ```sql
   CREATE ROLE smartnote LOGIN PASSWORD '...';
   CREATE DATABASE smartnote OWNER smartnote;
   -- either: the schema owned by the application user
   \c smartnote
   CREATE SCHEMA smartnote AUTHORIZATION smartnote;
   -- or: the right to create it, so that the first connection does
   GRANT CREATE ON DATABASE smartnote TO smartnote;
   ```

   A user who does not own an existing schema needs `GRANT USAGE, CREATE ON SCHEMA smartnote TO smartnote`.

3. SSH access: in **Settings → Keys** generate a key (Ed25519) or import your own, press
   **Export public**, and add the line to `~/.ssh/authorized_keys` of the SSH user on the server.
   A password works too; with a key, the password is tried after the key.

4. In **Settings** fill in the SSH server, the database (host `127.0.0.1`, `localhost` or `::1`
   as seen from the SSH server, port, name, user, password) and the sync phrase, press **Test**,
   then **OK**. On the first connection Smartnote asks whether to trust the server's host key
   and shows its SHA256 fingerprint; compare it with the host keys on the server,
   `for f in /etc/ssh/ssh_host_*_key.pub; do ssh-keygen -lf "$f"; done` (with OpenSSH defaults the
   key offered is usually the ECDSA one).

Every device that shares the notes uses the same sync phrase (at least 16 characters); the local
passwords may differ. The phrase never leaves the device: the notes are encrypted with a key derived
from it before they reach the server.

### Threat model

- The local storage is encrypted with a key protected by your password (Argon2id). Without the
  password the notes cannot be read; a weak password can be guessed offline by anyone who copies
  `local.db`.
- The server is trusted for integrity. It cannot read or forge a note, but it sees the number of
  notes, their UUIDs, dates and approximate sizes. It can delete a note, return an earlier ciphertext
  of the same note and change `updated`.
- The owner of the server can try to guess the sync phrase offline, so the phrase must be long.
- The connection is protected by SSH. The server's host key is checked against the one you trusted;
  a different key stops the connection, and the trusted key can be reset in Settings.

### Limitations

- Keys and passwords are wiped from memory where possible; the Go runtime may keep copies.
- Backup and Restore replace the whole set of notes on the other side, as in v1. Before deleting
  anything they ask, with the number of notes to delete.
- The sync phrase cannot be changed while the server holds notes.

### Building from source

Tools:

| Tool | Version |
|---|---|
| Go | 1.27.3 (`toolchain` in `go.mod`) |
| Node.js | LTS (developed with 24.21.0) |
| Wails CLI | v3.0.0: `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0` |
| Task | 3.54.0: `go install github.com/go-task/task/v3/cmd/task@v3.54.0` |
| golangci-lint | 2.15.0: `make install-lint` puts it in `bin/` |

On Ubuntu 26.04 the build also needs `build-essential pkg-config libgtk-4-dev
libwebkitgtk-6.0-dev` (`wails3 doctor` lists what is missing). The Windows installer needs NSIS
(`makensis`).

Day-to-day checks run through `make` (`make help` lists the targets); Wails builds and
packages run through Task, which `make build`, `make package` and `make smoke` call.

| Command | Purpose |
|---|---|
| `make all` | `dep`, `test` and `lint` |
| `make test` | Go unit tests with the race detector and total coverage |
| `make integration` | integration tests against PostgreSQL and OpenSSH in Docker |
| `make lint` | golangci-lint (installed to `bin/` at the pinned version) and govulncheck |
| `make fmt`, `make lint-fix` | format the code, apply automatic lint fixes |
| `npm --prefix frontend run test` | interface unit tests (Vitest) |
| `npm --prefix frontend run check`, `npm --prefix frontend run lint` | svelte-check and ESLint |
| `task dev` | run with live reload |
| `task build` | build `bin/smartnote` |
| `task package:linux` | `.deb` and AppImage in `bin/` |
| `task package:windows` | NSIS installer in `bin/` (on Windows) |
| `task package:darwin` | `.app` and `.dmg` in `bin/` (on macOS) |
| `task smoke:appimage` | start the AppImage in a clean `ubuntu:26.04` container (needs Docker) |

`docs/manual-test.md` is the acceptance checklist; `docs/smoke-windows-macos.md` covers the
unsigned Windows and macOS packages.

### License

MIT, see [LICENSE](LICENSE).
