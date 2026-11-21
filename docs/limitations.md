**English** | [Русский](limitations.ru.md)

# Limitations

Behaviour that may surprise a user who keeps notes on a server or works on several devices. For
users; the limits of the protection are in [Security](security.md#what-is-not-protected), those of
search by meaning in [Search](search.md#semantic-search).

- Backup and Restore replace the whole set of notes on the other side, as in v1. Before deleting
  anything they ask, with the number of notes to delete.
- The sync phrase cannot be changed while the server holds notes.
- Logging out or shutting down closes Smartnote without the close question, as SIGTERM does:
  unsaved server backups are not made, the local notes are kept. Back up before logging out.
- Linux: the file dialogs of **Import** and **Export public** are GTK's own, not the desktop's
  (xdg-desktop-portal), so they may look different from other dialogs of the desktop. The portal
  would need the process memory open to other programs of the user.
- Yandex Disk and Google Drive: two Backups made almost at the same moment from two devices can
  lose one of them. Both get the same revision, so the warning about an older storage does not
  notice it. Do not use two devices at the same time.
- Yandex Disk: the data belong to the application whose client ID signed you in. With your own
  client ID, or a build with another built-in one, the storage is another and empty; the notes
  under the old one stay there.
- Yandex Disk: the sign-in lasts about a year, then Smartnote asks to sign in again ("Sign in to
  Yandex Disk again."). The account name is not shown, only "Signed in".
- Yandex Disk: Smartnote cannot see or empty the Disk trash. If earlier copies of the container
  land there, they take space and can be deleted by hand in the Disk web interface.
- Yandex Disk and Google Drive: a large set of notes can take minutes to back up over a slow
  connection. An operation is limited to 10 minutes (60 seconds with an SSH + PostgreSQL server).
- The container holds at most 64 MiB. A larger set is refused with "Notes do not fit into the remote
  storage (64 MiB)."
- The sync phrase cannot be changed while the Yandex Disk or Google Drive storage holds notes, as
  with the server.
- After "The result of the backup is unknown. Connect again to check the remote storage." press
  **OK** in Settings (or restart Smartnote) to connect again: Smartnote first settles the unfinished write, and until it
  does it does not touch the storage.
- Yandex Disk: **Sign out** keeps the record of such an unfinished write, and the next sign-in to the
  same account settles it. Signing in to another account or with another client ID drops the
  record, because Smartnote cannot tell accounts apart; sign in to the same account first.
- The warning that the remote storage is older than this device last synchronized with applies to
  the server, Yandex Disk and Google Drive; see
  [Yandex Disk](yandex-disk.md#when-the-storage-is-older-than-the-device).
- Google Drive: the hidden folder belongs to the Google Cloud project of the client. Clients of one
  project share it; a client of another project, your own one included, has its own folder, which
  may already hold a copy of its own. Switching the client needs a new sign-in.
- Google Drive: the browser of the sign-in must run on the same computer as Smartnote. There is no
  manual code, so the sign-in cannot be finished on another computer or a phone. The account name
  is not shown, only "Signed in".
- Google Drive: the first connection to an empty storage makes the first Backup by itself. So the
  storage is created again while Google Drive is chosen on at least one device: delete the hidden
  app data in the order given in [Google Drive](google-drive.md#deleting-the-notes-from-google-drive).
- Google Drive: if two devices create the storage at the same moment, the second copy of the file
  stays until one of the next successful Backups removes it (at most 10 copies per Backup; a failed
  removal is tried again later).
- Google Drive: Drive may keep earlier revisions of the file (up to 30 days or 100 revisions). They
  hold only ciphertext and do not count toward the quota; Smartnote does not delete them.
- Google Drive: until the result of an unfinished upload is known, Smartnote starts no new write to
  Drive, and while Google cannot be reached the operations with Drive end with "The result of the
  backup is unknown…". **Sign out** and a change of the client keep the record of the upload; only
  a known result removes it.
- Google Drive: if a program deletes a single file in the hidden folder through the Drive API with a
  client of the same Google Cloud project while another device removes copies, the current file may
  be lost. The Google Drive interface cannot delete a single file there.
- Google Drive: the built-in client may be revoked by Google; then the sign-in works only with your
  own client (see [Your own client](google-drive.md#your-own-client)).
- Language of the interface: the technical texts of SSH and PostgreSQL errors come from the
  libraries and stay in English. A file dialog or a browser page after a sign-in that is already
  open keeps the language it was shown in.
- The privacy policy in the application is the copy of the published page made for the release; the
  page on the site may be newer (compare the effective dates).
- Going back from 2.3.0 to 2.2.x keeps `local.db` readable, with the notes and the Yandex Disk
  sign-in. 2.2.x does not know Google Drive: Connect fails, and its Settings let you choose another
  storage. Saving the Settings in 2.2.x drops your own Google client ID and secret; after returning
  to 2.3.0 enter the same pair again, and the saved Google sign-in works again unless Google revoked
  it. A new sign-in is needed only for another client.
- Going back from 2.2.0 to 2.1.0 keeps `local.db` readable. But 2.1.0 reads the saved Yandex Disk
  settings as incomplete PostgreSQL settings, and Connect fails with the validation error
  "invalid ssh_host: is required" (not an SSH network error). Clear the fields in the Settings of
  2.1.0; the local notes are not affected.
- After going back to 2.1.0 and returning to 2.2.0, the revisions Smartnote remembers for storages
  that 2.1.0 switched away from may be out of date. Usually that only means such a storage is
  compared as new. In a rare case (2.1.0 restored from a storage that had been rolled back, then
  switched to another one) Smartnote may warn that the storage is older when it is not, or not
  notice that another device saved to it, so that **Backup** overwrites that data without asking.
- The storage type **None (local only)** keeps the saved SSH + PostgreSQL fields, so that you can
  switch back without typing them again. After going back to 2.1.0 these fields make a configured
  server again, and 2.1.0 connects to PostgreSQL with them. As usual it compares the notes and asks
  before it replaces anything. Clear the fields in the Settings of 2.1.0 if you do not want that.
