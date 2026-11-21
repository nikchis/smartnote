**English** | [Русский](google-drive.ru.md)

# Google Drive

How to back up the notes to Google Drive and restore them from it. For users of Smartnote 2.3.0 and
later who have a Google account.

Google Drive is the third remote storage next to SSH + PostgreSQL and Yandex Disk. Connect, Backup
and Restore work as with the other storages, and the notes are encrypted on the device with a key
derived from your sync phrase: Google sees only ciphertext. Smartnote asks Google only for the
`https://www.googleapis.com/auth/drive.appdata` scope, the access to its own hidden application data
folder on your Drive; it gets no access to your files or to anything else in your Google account. In
that folder all notes live in one file, `smartnote.v1.bin`. The folder is not shown among your
files: Google Drive mentions it only as hidden app data in **Settings → Manage apps**.

## Connect

1. Open **Settings** and choose **Google Drive** under **Storage** (the others are **None (local
   only)**, **Yandex Disk** and **SSH + PostgreSQL**). The **Google account** group appears.
2. Press **Sign in with Google**. The browser opens the Google sign-in page; choose the account and
   allow the access to the hidden app data. Smartnote shows "Waiting for the browser…" for up to
   5 minutes; **Cancel** stops the wait.
3. When the browser shows "Signed in to Google Drive", return to Smartnote: **Settings** shows
   "Signed in". The account name is not shown: the access to the hidden folder does not let
   Smartnote read it.
4. Enter the sync phrase (at least 16 characters; every device that shares the notes uses the same
   one). The eye button next to the field shows the phrase so that you can check it, and hides it
   again; whenever Settings open, the phrase is hidden. Press **Test connection**, then **OK**. The
   sign-in is saved only when you press **OK**; **Cancel** or closing Settings discards it.

The first connection to an empty storage creates it and at once makes the first Backup of the notes
of this device. On another device choose **Google Drive**, sign in with the same account and enter
the same phrase: Smartnote offers to restore the notes.

The browser must run on the same computer as Smartnote. Google sends the result of the sign-in to
an address on `127.0.0.1` with a port chosen anew for each sign-in, and only a browser on this
computer reaches it. There is no manual code for Google, as there is for Yandex Disk: Google has
switched that kind of sign-in off for desktop applications. So a sign-in started in Smartnote cannot
be finished in a browser on another computer or on a phone.

**Sign out** forgets the sign-in to Google on this device when you press **OK**; the notes on Drive
stay, and you can restore them after signing in again. After that Smartnote does not connect to
Google Drive until you sign in again or choose another storage. While the sign-in is in progress
**Test connection** and **OK** are inactive, and closing Settings cancels it.

The sign-ins to Google Drive and to Yandex Disk are kept apart: switching the storage between them
needs no new sign-in, and **Sign out** in one of them leaves the other as it is.

## Your own client

The package carries the client ID and the client secret of the author's Desktop OAuth client in
Google Cloud, so normally you only press **Sign in with Google**. If you want the access to go
through your own client, create one. The names below are those of the English Google Cloud console
as of November 2026; Google may rename them.

1. In the Google Cloud console (console.cloud.google.com) create a project or choose one of yours.
2. In **APIs & Services → Library** enable the **Google Drive API**.
3. Open **Google Auth Platform** and set it up:
   - **Branding**: the app name and your support email;
   - **Audience**: the user type **External** (or **Internal** inside a Google Workspace
     organization). While the publishing status is **Testing**, add your Google account to
     **Test users**;
   - **Data Access**: add the scope `https://www.googleapis.com/auth/drive.appdata` and nothing else.
4. In **Clients** press **Create client**, choose the application type **Desktop app** and give it a
   name. Copy the **Client ID** (it ends with `.apps.googleusercontent.com`) and the
   **Client secret**. A Desktop client needs no redirect URI: Google accepts any port on `127.0.0.1`
   for it.
5. In Smartnote open **Settings → Advanced**, paste them into **Own client ID** and
   **Own client secret**, press **Sign in with Google** and then **OK**.

Both fields are needed: Google asks for the client secret when Smartnote exchanges the sign-in code
and whenever it renews the access, even for a Desktop client. Leave both empty to use the built-in
client. Your client secret is kept in the encrypted settings in `local.db`, like the passwords, and
is never written to the log.

The hidden folder belongs to the Google Cloud project of the client, not to the client itself.
Clients of the same project see the same folder and the same notes. A client of another project,
your own one included, has its own folder: it is empty, or holds the copy made earlier through that
project. The notes under the old client stay where they are. Switching between the built-in client
and your own one needs a new sign-in, because a sign-in is bound to the client ID.

While the publishing status of your app is **Testing**, Google ends the sign-ins after 7 days, and
Smartnote then asks you to sign in again (see [refresh token
expiration](https://developers.google.com/identity/protocols/oauth2#expiration) in the Google
documentation). To keep the sign-in, publish the app to production on the **Audience** page. If
Google then warns that the app is not verified, it is your own app, and you can continue.

## How long the sign-in lasts

Google gives an access that lasts an hour; Smartnote renews it by itself while you work, without a
new sign-in. The sign-in itself lasts until you revoke the access or sign out. Google also ends a
sign-in that has not been used for six months, and those of an app in the **Testing** status after 7
days ([refresh token
expiration](https://developers.google.com/identity/protocols/oauth2#expiration)). Then the next
operation shows "Sign in to Google Drive again." and, after Connect, opens **Settings**. Press
**Sign in again**, sign in once more and press **OK**; the notes are untouched.

## Large sets of notes, revisions and copies

An operation with Google Drive is limited to 10 minutes, as with Yandex Disk. In a test over an
ordinary connection an upload of 20 MiB took under a minute, but a large set of notes can take
minutes over a slow one; during a transfer Smartnote shows "This can take several minutes on a slow
connection. Do not close Smartnote.".

Google Drive may keep earlier revisions of the file (up to 30 days or 100 revisions). They hold only
ciphertext, and only the current version counts toward your storage quota. Smartnote does not
delete them; deleting the hidden app data (below) removes the file, and Drive handles its revisions
by its own rules.

If two devices create the storage at the same moment, a second copy of the file may stay in the
folder for a while. One of the next successful Backups removes it.

## Revoking access

Open myaccount.google.com/permissions, find Smartnote (or the name of your own app) and remove its
access. The next time Smartnote talks to Drive it shows "Sign in to Google Drive again."; after
Connect it opens **Settings** by itself. In **Settings** press **Sign in again**, sign in once more
and press **OK**. The notes on Drive and on the device are untouched.

## Deleting the notes from Google Drive

1. On every device that uses Google Drive, choose another storage in **Settings** (for example
   **None (local only)**), or press **Sign out**, and press **OK**. Otherwise the next connection of
   such a device finds the storage empty and makes the first Backup again.
2. Open the settings of Google Drive (drive.google.com, the gear button, **Settings**), choose
   **Manage apps**, find Smartnote (or the name of your own app) and, in its menu, choose
   **Delete hidden app data**. The names of the Drive menu are given as of November 2026.

This deletes the hidden folder of Smartnote with the file. The notes on your devices stay.
Uninstalling Smartnote and deleting its data folder removes what it stored on a device.

## When the storage is older than the device

The warning about a storage that holds an older version of the notes than this device last
synchronized with works for Google Drive as for Yandex Disk; see
[Yandex Disk](yandex-disk.md#when-the-storage-is-older-than-the-device).

## Switching storages

Smartnote synchronizes with one storage at a time, the one chosen in **Settings**. You can switch
between Google Drive, Yandex Disk, SSH + PostgreSQL and **None (local only)**: the fields and the
sign-ins of the other types are kept. The device remembers the revision it last synchronized with
in each storage, so a storage that has not changed since then is not offered as fresher data, as
described in
[Yandex Disk](yandex-disk.md#switching-between-yandex-disk-and-ssh--postgresql). Smartnote does not
merge storages: to move the notes to Google Drive, choose it and press **Backup**.

See also [Security](security.md#google-drive) for what Google can see and do, and
[Limitations](limitations.md) for the file size limit and other restrictions.
