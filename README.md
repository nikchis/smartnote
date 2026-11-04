# Smartnote

A notepad for private records on Linux, Windows and macOS. Notes are kept in an encrypted local
storage; optionally they are backed up to and restored from your own PostgreSQL server reachable
over SSH. Version 2 is a rewrite of the Delphi application (branch
[`v1`](https://github.com/nikchis/smartnote/tree/v1)) in Go, Wails v3 and Svelte.

## Features

- Notes encrypted locally with a key protected by your password, with a version history.
- Optional backup and restore through your own PostgreSQL over SSH; the server sees only ciphertext.
- Local full-text and semantic search, in Russian and English and in code; nothing leaves the device.

## Download

Get the package from the [releases page](https://github.com/nikchis/smartnote/releases); the
packages are not signed. Steps and requirements are in
[Installation and upgrade](docs/install.md).

| System | Package |
|---|---|
| Ubuntu 26.04 (amd64) | `smartnote.deb` |
| Ubuntu desktop (amd64) | `smartnote-x86_64.AppImage` |
| Windows 10/11 | `smartnote-amd64-installer.exe` |
| macOS 12+ on Apple Silicon | `smartnote.dmg` |

## Documentation

All documents are in [docs/](docs/README.md), in English and Russian.

- [Installation and upgrade](docs/install.md): per system, and the upgrade from 2.0.x.
- [Server setup](docs/server-setup.md): PostgreSQL and OpenSSH for synchronization.
- [Search](docs/search.md): how to search and what the status line means.
- [Security](docs/security.md): the threat model and what is not protected.
- [Limitations](docs/limitations.md): behaviour to know before relying on the server.
- [Building from source](docs/building.md): tools and commands for developers.

## License

MIT, see [LICENSE](LICENSE). Third-party components: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
