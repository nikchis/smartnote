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
`io_uring` and writing extended file attributes. The kernel decides how much of it works:
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
