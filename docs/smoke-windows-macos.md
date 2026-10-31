# Smoke test on Windows and macOS

The packages for Windows (`smartnote-amd64-installer.exe`) and macOS (`smartnote.dmg`) are built by CI
(job `package`, artifacts `smartnote-windows` and `smartnote-darwin`) and are not signed. This check
is run by hand on each system; the **Result** and **Checked by** columns are filled in by the tester.

The server is the one from `docs/manual-test.md` (OpenSSH and PostgreSQL containers), reachable from
the test machine.

## Windows 10/11

| # | Step | Expected | Result | Checked by |
|---|---|---|---|---|
| 1 | Run the installer; on "Windows protected your PC" choose **More info** → **Run anyway**. | The installer finishes; Smartnote is in the Start menu. | | |
| 2 | Start Smartnote. | The New storage screen opens; light or dark theme follows the system. | | |
| 3 | Create the storage, add a note. | "Note has been successfully added."; `local.db` is in `%APPDATA%\smartnote`. | | |
| 4 | **Settings**: generate a key, add the public key to the server, fill in the server, **Test**, trust the key. | "Connection successfully established." (or "… Remote storage is empty."). | | |
| 5 | **OK**, edit a note, **Backup**. | "Data successfully saved." | | |
| 6 | Log in as a second Windows user, install or start Smartnote, create a storage, configure the same server and phrase. | The Restore question; Yes shows "Data successfully restored." and the notes of the first user. | | |
| 7 | Edit a note, close the window. | "Would you like to save your data changes to remote database?"; Yes shows "Data successfully synchronized." and the app quits. | | |
| 8 | Start Smartnote twice. | The second start brings the open window forward. | | |

## macOS

| # | Step | Expected | Result | Checked by |
|---|---|---|---|---|
| 1 | Open the `.dmg`, drag Smartnote to Applications, start it; when Gatekeeper refuses, open **System Settings → Privacy & Security** → **Open Anyway** (or `xattr -dr com.apple.quarantine /Applications/smartnote.app`). | Smartnote starts. | | |
| 2 | Create the storage, add a note. | "Note has been successfully added."; `local.db` is in `~/Library/Application Support/smartnote`. | | |
| 3 | **Settings**: generate a key, add the public key to the server, fill in the server, **Test**, trust the key. | "Connection successfully established." | | |
| 4 | **OK**, edit a note, **Backup**. | "Data successfully saved." | | |
| 5 | Log in as a second macOS user, start Smartnote, create a storage, configure the same server and phrase. | The Restore question; Yes shows "Data successfully restored." | | |
| 6 | Edit a note, quit with Cmd+Q. | The save question appears before quitting; after Yes, "Data successfully synchronized." | | |
| 7 | Start Smartnote again from Applications while it runs. | The open window comes to the front. | | |
