**English** | [Русский](yandex-disk.ru.md)

# Yandex Disk

How to back up the notes to Yandex Disk and restore them from it instead of your own server. For
users of Smartnote 2.2.0 and later who have a Yandex account.

Yandex Disk is the second remote storage next to SSH + PostgreSQL. Connect, Backup and Restore work
as with the server, and the notes are encrypted on the device with a key derived from your sync
phrase: Yandex sees only ciphertext. All notes live in one file in the application folder of your
Disk, `Applications/<app name>/smartnote.v1.bin`.

## Connect

1. Open **Settings** and choose **Yandex Disk** in the storage choice (the others are **None (local
   only)** and **SSH + PostgreSQL**). The SSH and database fields disappear.
2. Press **Sign in with Yandex**. The browser opens on the Yandex page; sign in and press **Allow**.
   Smartnote asks only for access to its own application folder, not to the rest of the Disk.
   Smartnote shows "Waiting for the browser…" for up to 5 minutes; **Cancel** stops the wait.
3. When the browser reports success, **Settings** shows "Signed in as <login>".
4. Enter the sync phrase (at least 16 characters; every device that shares the notes uses the same
   one). Press **Test connection**, then **OK**.

The first connection to an empty Disk creates the storage and makes the first Backup. On another
device choose **Yandex Disk**, sign in with the same account and enter the same phrase: Smartnote
offers to restore the notes.

If the browser cannot reach Smartnote (the port 47123 on `127.0.0.1` is busy, or the browser runs
on another machine), press **Enter code manually**: the browser opens a Yandex page that shows a
code. Paste it into the field and press **Submit**.

**Sign out** forgets the sign-in on this device; the notes on the Disk stay. While the sign-in is
in progress **Test connection** and **OK** are inactive, and closing Settings cancels it.

## Your own client ID

The package carries the client ID of the author's Yandex application, so normally you only press
**Sign in with Yandex**. If you want the access to go through your own application, register one:

1. Open the Yandex ID developer console (oauth.yandex.com) and create an application:
   - type: for user authorization;
   - platform: Web services;
   - permission: access to the app folder on Disk (`cloud_api:disk.app_folder`) and nothing else;
   - Redirect URIs: `http://127.0.0.1:47123/callback` and
     `https://oauth.yandex.ru/verification_code`.
2. Copy the ID of the application, 1 to 64 characters from letters, digits, `-` and `_`.
3. In **Settings → Advanced** paste it into **Own client ID**, sign in again and press **OK**.

Smartnote needs no client secret. Data belongs to the application: another client ID means another
application folder, that is another, empty storage. Your notes under the old client ID stay where
they are. Changing the client ID signs you out.

## Revoking access

Open your Yandex ID account, the page of data access (Data access, "Доступы" in Russian), and
revoke the access of the application. The next time Smartnote talks to the Disk it shows
"Sign in to Yandex Disk again."; press **Sign in with Yandex** and sign in once more. The notes in
the Disk and on the device are untouched.

## When the storage is older than the device

If the storage holds an older version of the notes than this device last synchronized with (for
example, the file was replaced with an earlier copy), Connect warns: "The remote storage holds an
older version of the data than this device last synchronized with. It may have been restored from
an earlier copy. Use Backup to overwrite it, or Restore to replace your notes with it." Nothing is
replaced until you choose. The Restore question then starts with the same warning. The same
warning exists for SSH + PostgreSQL. It appears only when the two sets of notes differ; with equal
sets there is nothing to lose and Smartnote continues quietly.

See also [Security](security.md#yandex-disk) for what Yandex can see and do, and
[Limitations](limitations.md) for the file size limit and other restrictions.
