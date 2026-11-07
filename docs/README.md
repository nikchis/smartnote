**English** | [Русский](README.ru.md)

# Smartnote documentation

The index of the Smartnote documentation: guides for users first, then notes for developers who
build, package and test the application. Every document has a Russian version.

## For users

- [Installation and upgrade](install.md): packages for Linux, Windows and macOS, what semantic
  search needs, the upgrade from 2.1.0, the one-way upgrade from 2.0.x and how to go back, where the
  files are.
- [Server setup](server-setup.md): PostgreSQL and OpenSSH for backing up and restoring the notes,
  and the sync phrase.
- [Yandex Disk](yandex-disk.md): sign-in, your own client ID, revoking access.
- [Search](search.md): words, phrases, fragments of code and search by meaning; the status line.
- [Security](security.md): the threat model, process hardening, the isolation of the search worker
  and what is not protected.
- [Limitations](limitations.md): Backup and Restore, the sync phrase, logging out, Yandex Disk and
  going back to 2.1.0.

## For developers

- [Building from source](building.md): tools and their versions, system packages, `make` and
  `task` commands.
- [Development notes](development.md): the layout of the code and the checks of bindings, errors,
  the Content Security Policy and the packages in a clean container.
- [Packaging](packaging.md): what each package ships with the search, fetching the model and ONNX
  Runtime, licence notices, signing on macOS, CI.
- [Search maintenance](search-maintenance.md): the storage format, worker restarts, what the worker
  may log, AddressSanitizer and CI checks.
- [Manual acceptance test](manual-test.md): scenarios of the application and the search with
  recorded results.
- [Smoke test on Windows and macOS](smoke-windows-macos.md): checklist for the unsigned packages.

## Component notes (English only)

- [tools/model/README.md](../tools/model/README.md): how the search model is prepared, built and
  fetched.
- [search/testdata/protocol/README.md](../search/testdata/protocol/README.md): CBOR samples of the
  worker protocol and the encoding rules both sides follow.
- [THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md): licences of the third-party components the
  packages ship.
