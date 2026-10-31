# Manual acceptance test

The scenarios of the specification (section 7.4). Run them on a clean Ubuntu 26.04 desktop, once from
`smartnote.deb` and once from `smartnote-x86_64.AppImage`. The server is a pair of containers: the
OpenSSH image from `internal/adapter/ssh/testdata/sshd` (user `tester`, password `testpass`, TCP
forwarding allowed) and `postgres:17-alpine` started with `--network container:<sshd>` and
`listen_addresses=127.0.0.1`, so the SSH server reaches it as `127.0.0.1`.

Mark each scenario in the **Result** column: `pass`, `fail` (with a note) or `not run`.
Remove `~/.local/share/smartnote` before scenario 1 to start without a storage.

| # | Scenario | Steps | Expected | .deb | AppImage |
|---|---|---|---|---|---|
| 1 | First start | Start Smartnote. Type a 5-character password twice; then 6 characters with a different confirmation; then 6 equal characters and press **Create storage**. | The New storage screen opens. **Create storage** is inactive for 5 characters and for different inputs, active for 6–64 equal ones. After creation the Main screen opens with an empty list. | not run | pass |
| 2 | Unlock | Quit and start again. Enter a wrong password and press Enter; then the right one. | Wrong: "Access to local storage denied.", the field is cleared. Right: the Main screen with the notes. | not run | pass |
| 3 | New and Cancel | **New** → **Cancel** without typing. **New**, type only a name → **Cancel**. **New**, type a name and a text → **Apply**. | During New only **Cancel** is active (Apply needs both fields); the list, New and Delete are inactive. Cancel returns to the previous state. Apply shows "Note has been successfully added." and the note appears in the list. | not run | pass |
| 4 | Edit and History | Change the text of a note → **Apply**. **History** → choose the version → **Use** → **Apply**. | "Note has been successfully updated." History lists the previous text with its date; Use puts it into the editor without saving; Apply saves it and History now has one more version. | not run | pass |
| 5 | Rename | Change only the name → **Apply**. | The note is renamed; History gets no new version. | not run | pass |
| 6 | Delete | **Delete** → **No**; **Delete** → **Yes**. | "Are you sure you want to remove this note?"; No keeps the note; Yes shows "Note has been successfully removed." and removes the note with its history. | not run | pass |
| 7 | Lengths | Paste a name of 64 Cyrillic letters, then one more letter; a text with emoji up to 8192 units. | The name stops at 64; the counter shows `n/8192`, an emoji counts as 2. | not run | partly: counter `8/8192` for «Текст 😀» checked, the 64/8192 limits not |
| 8 | Change password | **Change password**, enter a new one twice → **OK**. Quit, start, try the old and the new password. | "Password was successfully changed." The old password is denied, the new one opens the same notes. | not run | pass |
| 9 | Keys | **Settings** → **Generate** (twice) → **Export public** → **Import** an OpenSSH key, then a key protected by a passphrase (wrong passphrase first). | The second Generate asks "Are you sure you want to generate new key? Current key will be erased!", then "New key successfully generated.". Export copies the key to the clipboard and saves the file. A protected key asks for its passphrase again after a wrong one, without reopening the file dialog. | not run | pass (wrong passphrase asked again, one file dialog) |
| 10 | Test and trust | Fill in the server settings, **Test**, answer **Yes** to the host key question; **Cancel**. Open Settings again, **Test**. | Test asks to trust the key with its SHA256 fingerprint and address, then "Connection successfully established. Remote storage is empty.". After Cancel the key is not trusted: Test asks again. | not run | pass |
| 11 | First Backup | **Settings** → **Test** → trust → **OK** on an empty server. | The schema is created and the notes are backed up without a question; Backup and Restore stay inactive (nothing to save). | not run | pass (server revision 1) |
| 12 | Second device | On another user account (another password, the same phrase) configure the same server → **OK**. | One question: "It seems like your remote storage has fresher version of data. Would you like to restore data from remote storage?"; Yes shows "Data successfully restored." and the same notes. | not run | pass (second Linux user) |
| 13 | Wrong phrase | Change the phrase to another 16+ characters → **OK**. | An error about the sync phrase, and Settings open again. The server data are unchanged (check from the first device). | not run | pass (server revision unchanged) |
| 14 | Changed host key | Recreate the SSH container with new host keys; connect. **Settings** → **Reset** → **OK**; connect again. | The connection is refused with both fingerprints. After Reset the key is asked about again and the connection works. | not run | pass |
| 15 | Backup with deletions | Delete two notes → **Backup**. | "Would you like to save data on remote storage?" with "Backup will delete 2 notes on the server."; Yes shows "Data successfully saved.". | not run | pass |
| 16 | Restore with deletions | On the second device add a note and Apply. On the first device change a note → **Backup**. On the second device open **Settings** → **OK** (the connect check) and answer **No**, then press **Restore**. | "Would you like to restore data from remote storage?" with "Restore will delete 1 local notes."; Yes shows "Data successfully restored.". | not run | pass |
| 17 | Close with changes | Edit a note, Apply, close the window → **Yes**. Repeat with the SSH container stopped. | "Would you like to save your data changes to remote database?"; Yes saves, shows "Data successfully synchronized." and quits. With the server down the error is shown, then "Close anyway?": No keeps the window, Yes quits. | not run | pass (both branches) |
| 18 | Local only | Clear every field in Settings → **OK**. | No connection is attempted; Backup and Restore are inactive; notes still work. | not run | pass |
| 19 | Second instance | Start Smartnote while it runs. | The running window comes to the front; no second window, the storage is untouched. | not run | pass (second process exits; focus not observable under Xvfb) |
| 20 | Network break | Start a Backup and stop the SSH container during it (`docker pause`). | An error (timeout or SSH); Backup and Restore become inactive; no notes are lost, the next Backup works after the server is back. | not run | not run |

## Results

Tested by: Claude Code (scripted run) Date: 2026-10-31 Package versions: branch `v2-15-ci-docs`, AppImage built by `task package:linux`

How: the AppImage in a clean `ubuntu:26.04` container under Xvfb with openbox, driven by `xdotool`,
checked on screenshots; the server was the OpenSSH test image and `postgres:17-alpine` sharing its
network namespace. The `.deb` was not installed. Scenario 20 was not run; the break during Backup
is covered by the integration test `TestBackupCommitAckLost` and the use case tests (Review Focus 3).

Failures and notes:
- No failures.
- Closing with a failed Backup showed the raw host key mismatch text; fixed so that it shows both
  fingerprints as the connect check does.
- The trust question shows the key the server offers first, with OpenSSH defaults the ECDSA key;
  README now says to compare with every host key.
- WebKitGTK keeps its own `storage` and `mediakeys` directories in the data directory
  (`~/.local/share/smartnote`); the app stores nothing there.
