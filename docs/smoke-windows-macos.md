**English** | [Русский](smoke-windows-macos.ru.md)

# Smoke test on Windows and macOS

A checklist for the unsigned Windows and macOS packages, including the search checks that CI
cannot do. For testers with access to those systems.

The packages for Windows (`smartnote-amd64.msi`) and macOS (`smartnote.dmg`, Apple Silicon
only) are built by CI
(job `package`, artifacts `smartnote-windows` and `smartnote-darwin`) and are not signed. This check
is run by hand on each system; the **Result** and **Checked by** columns are filled in by the tester.

The server is the one from [Manual acceptance test](manual-test.md) (OpenSSH and PostgreSQL
containers), reachable from the test machine.

The search steps check what CI cannot: on Windows and macOS the search worker `smartnote-search` runs
without the Linux sandbox, so the tester confirms that it ends with the application, opens no network
connections and that ONNX Runtime writes no telemetry files (`onnxruntime.db`, `deviceid`) into the
user's profile (specification, sections 6.1 and 14). Note the time before the first start of
Smartnote: files older than that are not from this test.

## Windows 10/11

| # | Step | Expected | Result | Checked by |
|---|---|---|---|---|
| 1 | Run the `.msi`; on "Windows protected your PC" choose **More info** → **Run anyway**. | No administrator prompt; the installation finishes; Smartnote is in the Start menu and in `%LOCALAPPDATA%\Programs\Smartnote`. | | |
| 2 | Start Smartnote. | The New storage screen opens in the dark v1 colors, with a light or a dark system theme alike. The Smartnote logo is on `smartnote.exe` in Explorer, in the title bar and on the taskbar. | | |
| 3 | Create the storage, add a note. | "Note has been successfully added."; `local.db` is in `%APPDATA%\smartnote`. | | |
| 4 | **Settings**: generate a key, add the public key to the server, fill in the server, **Test connection**, trust the key. | "Connection successfully established." (or "… Remote storage is empty."). | | |
| 5 | **OK**, edit a note, **Backup**. | "Data successfully saved." | | |
| 6 | Log in as a second Windows user, install or start Smartnote, create a storage, configure the same server and phrase. | The Restore question; Yes shows "Data successfully restored." and the notes of the first user. | | |
| 7 | Edit a note, close the window. | "Would you like to save your data changes to remote storage?"; Yes shows "Data successfully synchronized." and the app quits. | | |
| 8 | Start Smartnote twice. | The second start brings the open window forward. | | |
| 9 | Edit a note, sign out of Windows with Smartnote open. | Windows signs out without waiting for Smartnote; after signing in, the note is there. | | |
| 10 | Add a note "Pancakes" with the text "flour, milk, eggs". Press Ctrl+F, type `milk`, then `recipe for crepes`. | `smartnote-search.exe` and `onnxruntime.dll` are in the installation folder, with a `model` folder. Neither "Search unavailable" nor, a few seconds after the start, "Semantic search unavailable" appears; both queries list "Pancakes", and Enter on a hit jumps to its line. | | |
| 11 | With Smartnote open, in PowerShell: `Get-NetTCPConnection -OwningProcess (Get-Process smartnote-search).Id; Get-NetUDPEndpoint -OwningProcess (Get-Process smartnote-search).Id`. | `smartnote-search` runs; both commands list nothing (an error "No matching MSFT_NetTCPConnection objects" is the expected empty result). | | |
| 12 | Close Smartnote, then in PowerShell: `Get-Process smartnote-search`. | No such process. | | |
| 13 | In PowerShell: `Get-ChildItem $env:USERPROFILE -Recurse -Force -ErrorAction SilentlyContinue -Include onnxruntime.db, deviceid* \| Select-Object FullName, LastWriteTime`. | Nothing written after the start of the test. | | |
| 14 | On a clean Windows without the Microsoft Visual C++ Redistributable (x64) (**Settings → Apps** lists no "Microsoft Visual C++ 2015-2022 Redistributable (x64)"; a fresh virtual machine), install and start Smartnote, add the note of step 10, search `milk`; look at `%LOCALAPPDATA%\smartnote\smartnote.log`. Then install the Redistributable (`vc_redist.x64.exe` from Microsoft), restart Smartnote and search `recipe for crepes`. | Without the Redistributable: `milk` lists "Pancakes" (full-text search works), "Semantic search unavailable" is shown, and the log has the reason "semantic search unavailable: loading the ONNX Runtime library failed". After installing it: no "Semantic search unavailable" once the model has loaded, and `recipe for crepes` lists "Pancakes". | | |

## macOS

| # | Step | Expected | Result | Checked by |
|---|---|---|---|---|
| 1 | Open the `.dmg`, drag Smartnote to Applications, start it; when Gatekeeper refuses, open **System Settings → Privacy & Security** → **Open Anyway** (or `xattr -dr com.apple.quarantine /Applications/smartnote.app`). | Smartnote starts; the Smartnote logo is in the Dock. | | |
| 2 | Create the storage, add a note. | "Note has been successfully added."; `local.db` is in `~/Library/Application Support/smartnote`. | | |
| 3 | **Settings**: generate a key, add the public key to the server, fill in the server, **Test connection**, trust the key. | "Connection successfully established." | | |
| 4 | **OK**, edit a note, **Backup**. | "Data successfully saved." | | |
| 5 | Log in as a second macOS user, start Smartnote, create a storage, configure the same server and phrase. | The Restore question; Yes shows "Data successfully restored." | | |
| 6 | Edit a note, quit with Cmd+Q. | The save question appears before quitting; after Yes, "Data successfully synchronized." | | |
| 7 | Start Smartnote again from Applications while it runs. | The open window comes to the front. | | |
| 8 | Edit a note, **Apple menu → Log Out** with Smartnote open. | macOS logs out without the save question and without "Smartnote canceled logout"; after logging in, the note is there. | | |
| 9 | `codesign --verify --deep --strict --verbose=2 /Applications/smartnote.app` | "valid on disk" and "satisfies its Designated Requirement". | | |
| 10 | Add a note "Pancakes" with the text "flour, milk, eggs". Press Cmd+F, type `milk`, then `recipe for crepes`. | `smartnote-search` and `libonnxruntime.1.31.0.dylib` are in `smartnote.app/Contents/MacOS`, the model in `Contents/Resources/model`. Neither "Search unavailable" nor, a few seconds after the start, "Semantic search unavailable" appears; both queries list "Pancakes", and Enter on a hit jumps to its line. | | |
| 11 | With Smartnote open, in Terminal: `lsof -nP -i -a -p "$(pgrep -x smartnote-search)"`. | `pgrep` finds the worker; `lsof` lists no connections. | | |
| 12 | Quit Smartnote with Cmd+Q, then `pgrep -lx smartnote-search`. | No output. | | |
| 13 | `find ~ \( -name onnxruntime.db -o -name 'deviceid*' \) -newermt "<start time>" 2>/dev/null` with the time noted before the test, e.g. `"2026-11-05 10:00"`. | No output. | | |
