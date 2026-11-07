**English** | [Русский](security.ru.md)

# Security

What Smartnote protects, from whom, and what it does not protect: the threat model of the local
storage and the server, the hardening of the processes and the isolation of the search worker. For
users who decide what to keep in Smartnote and where to run its server.

## Threat model

- The local storage is encrypted with a key protected by your password (Argon2id). Without the
  password the notes cannot be read; a weak password can be guessed offline by anyone who copies
  `local.db`.
- The server is trusted for integrity. It cannot read or forge a note, but it sees the number of
  notes, their UUIDs, dates and approximate sizes. It can delete a note, return an earlier ciphertext
  of the same note and change `updated`.
- The owner of the server can try to guess the sync phrase offline, so the phrase must be long.
- The connection is protected by SSH. The server's host key is checked against the one you trusted;
  a different key stops the connection, and the trusted key can be reset in Settings.

## Yandex Disk

How [Yandex Disk](yandex-disk.md) as the remote storage differs from the server; the notes are
encrypted in the same way.

- Yandex sees your account, the IP address, the moments of writing, and one file with its size to
  within 64 KiB. It does not see the number of notes, their UUIDs, dates or sizes one by one.
- Yandex, or anyone with access to your account, can delete the file, bring back an earlier version
  of it and try to guess the sync phrase offline from the file header, so the phrase must be at
  least 16 characters (the key is derived with Argon2id, t=3, m=64 MiB). It cannot read or forge a
  note: XChaCha20-Poly1305 protects the notes, the revision and `updated`, and the associated data
  hold the storage identifier and the hash of the header.
- When an earlier version brings back a set of notes that differs from yours and its revision is
  lower than the one this device last synchronized with, Smartnote warns (see
  [When the storage is older than the device](yandex-disk.md#when-the-storage-is-older-than-the-device)).
  With equal sets nothing is lost. Two writes that end with the same revision are not told apart:
  see [Limitations](limitations.md).
- Sign-in: a program that took the port of the sign-in callback first gains nothing without the
  code verifier of the sign-in (PKCE). Every callback is checked against `state` first, so a foreign
  page cannot cancel your sign-in. The receiver listens on `127.0.0.1` only and closes after the
  first correct callback.
- The access token is stored in `local.db` encrypted with the same key as the notes. It is sent
  only to `cloud-api.yandex.net`, never to the upload and download servers. In memory it is treated
  like the other secrets (wiped where possible). Tokens, codes, `state` and verifiers are never
  written to the log.
- The connection uses TLS with the root certificates of the system and no key pinning, unlike the
  pinned host key of SSH.
- The client ID of the application is public, and the package holds no application secret.

## Search data

- The search index exists only in the memory of the search worker `smartnote-search`; there is no
  plain text of the notes on disk.
- The vectors of semantic search are kept in `local.db`, encrypted like the notes; they are not sent
  to the server.
- The worker talks to the application only through its standard input and output. The log
  `smartnote.log` gets no note text and no query from it, only a fixed set of short messages.
- The worker checks the SHA-256 of the model files and of the ONNX Runtime library against the
  values built into it before it uses them, and turns the telemetry of ONNX Runtime off.

## Process hardening

| System | Application and worker |
|---|---|
| Linux | no core dumps; other processes of the same user cannot attach a debugger or read the memory (`PR_SET_DUMPABLE`) |
| macOS | no core dumps; a debugger cannot attach (`PT_DENY_ATTACH`) |
| Windows | crash reports without the heap contents; no images loaded from remote shares and low-integrity locations |

The worker clears every block of memory it frees.

On Linux the worker also isolates itself before it receives the first note, irreversibly: Landlock
forbids it any change to the file system and TCP connections, seccomp forbids new sockets,
`io_uring` and changing file metadata (extended attributes, mode, owner and times). The kernel decides how much of it works:
`sandbox=full` needs Linux 6.10 or newer, Linux 6.7–6.9 gives `partial`, and a lower level is
written to `smartnote.log` as a warning. The search works at every level.

## What is not protected

- On Windows and macOS the search worker is not isolated from the disk and the network.
- Keys and passwords are wiped from memory where possible; the Go runtime may keep copies. The text
  of the notes is also in the memory of the application, of the WebView and of ONNX Runtime.
- The WebView processes (WebKitGTK on Linux) are not hardened by Smartnote: other processes of the
  same user can read their memory.
- Swap and hibernation can write the memory of the processes to disk; use the disk and swap
  encryption of the operating system.
- What the server can see and do is listed in the [threat model](#threat-model).
