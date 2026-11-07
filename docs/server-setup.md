**English** | [Русский](server-setup.ru.md)

# Server setup

How to prepare your own server so that Smartnote backs up the notes to it and restores them from it.
For users who want to share notes between devices or keep a copy off the computer.

Synchronization is optional: with the storage **None (local only)** in **Settings** Smartnote works
locally only. Without a server of your own the notes can be backed up to
[Yandex Disk](yandex-disk.md) instead. The server needs OpenSSH and PostgreSQL; Smartnote connects
over SSH and reaches PostgreSQL through the SSH connection, so the database never has to listen on
a public address.

## Steps

1. PostgreSQL listens on loopback only (`postgresql.conf`):

   ```
   listen_addresses = 'localhost'
   ```

   and `pg_hba.conf` lets the user in from loopback with a password, e.g.
   `host smartnote smartnote 127.0.0.1/32 scram-sha-256` (and `host smartnote smartnote ::1/128
   scram-sha-256` for the database host `localhost` or `::1`).

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

3. Open **Settings** and choose **SSH + PostgreSQL** under **Storage**: only then the fields of the
   server and the **Keys** group appear. Settings that 2.1.0 saved with a server open with this
   choice already.

4. SSH access: in **Settings → Keys** generate a key (Ed25519) or import your own, press
   **Export public**, and add the line to `~/.ssh/authorized_keys` of the SSH user on the server.
   A password works too; with a key, the password is tried after the key.

5. In **Settings** fill in the SSH server, the database (host `127.0.0.1`, `localhost` or `::1`
   as seen from the SSH server, port, name, user, password) and the sync phrase; empty ports
   mean 22 for SSH and 5432 for PostgreSQL, an empty database host means `127.0.0.1`. Press
   **Test connection**, then **OK**. On the first connection Smartnote asks whether to trust the
   server's host key and shows its SHA256 fingerprint; compare it with the host keys on the server,
   `for f in /etc/ssh/ssh_host_*_key.pub; do ssh-keygen -lf "$f"; done` (with OpenSSH defaults the
   key offered is usually the ECDSA one).

## Sync phrase

Every device that shares the notes uses the same sync phrase (at least 16 characters); the local
passwords may differ. The phrase never leaves the device: the notes are encrypted with a key derived
from it before they reach the server.

What the server can and cannot learn is described in [Security](security.md#threat-model); how
Backup and Restore treat the notes on the other side, in [Limitations](limitations.md).
