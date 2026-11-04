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
| 1 | First start | Start Smartnote. Type a 5-character password twice; then 6 characters with a different confirmation; then 6 equal characters and press **Create storage**. | The New storage screen opens. **Create storage** is inactive for 5 characters and for different inputs, active for 6–64 equal ones. After creation the Main screen opens with an empty list. | pass (installed with `apt install` in a clean `ubuntu:26.04`) | pass |
| 2 | Unlock | Quit and start again. Enter a wrong password and press Enter; then the right one. | Wrong: "Access to local storage denied.", the field is cleared. Right: the Main screen with the notes; the first note is selected and has the keyboard focus, ↑/↓ move the selection through the list. | not run | pass |
| 3 | New and Cancel | **New** → **Cancel changes** without typing. **New**, type only a name → **Cancel changes**. **New**, type a name and a text → **Apply changes**. | During New only **Cancel changes** is active (Apply needs both fields); the list, New and Remove are inactive. Cancel returns to the previous state. Apply shows "Note has been successfully added." and the note appears in the list. | not run | pass |
| 4 | Edit and History | Change the text of a note → **Apply changes**. **History** → choose the version → **Use** → **Apply changes**. | "Note has been successfully updated." History lists the previous text with its date; Use puts it into the editor without saving; Apply saves it and History now has one more version. | not run | pass |
| 5 | Rename | Change only the name → **Apply changes**. | The note is renamed; History gets no new version. | not run | pass |
| 6 | Delete | **Remove** → **No**; **Remove** → **Yes**. | "Are you sure you want to remove this note?"; No keeps the note; Yes shows "Note has been successfully removed." and removes the note with its history. | not run | pass |
| 7 | Lengths | Paste a name of 64 Cyrillic letters, then one more letter; a text with emoji up to 8192 units. | The name stops at 64; the counter shows `n/8192`, an emoji counts as 2. | not run | partly: counter `8/8192` for «Текст 😀» checked, the 64/8192 limits not |
| 8 | Change password | The key button (**Set new password**), enter a new one twice → **OK**. Quit, start, try the old and the new password. | "Password was successfully changed." The old password is denied, the new one opens the same notes. | not run | pass |
| 9 | Keys | **Settings** → **Generate** (twice) → **Export public** → **Import** an OpenSSH key, then a key protected by a passphrase (wrong passphrase first). | The second Generate asks "Are you sure you want to generate new key? Current key will be erased!", then "New key successfully generated.". Export copies the key to the clipboard and saves the file. A protected key asks for its passphrase again after a wrong one, without reopening the file dialog. | not run | pass (wrong passphrase asked again, one file dialog) |
| 10 | Test and trust | Fill in the server settings, **Test connection**, answer **Yes** to the host key question; **Cancel**. Open Settings again, **Test connection**. | Test asks to trust the key with its SHA256 fingerprint and address, then "Connection successfully established. Remote storage is empty.". After Cancel the key is not trusted: Test asks again. | not run | pass |
| 11 | First Backup | **Settings** → **Test connection** → trust → **OK** on an empty server. | The schema is created and the notes are backed up without a question; Backup and Restore stay inactive (nothing to save). | not run | pass (server revision 1) |
| 12 | Second device | On another user account (another password, the same phrase) configure the same server → **OK**. | One question: "It seems like your remote storage has fresher version of data. Would you like to restore data from remote storage?"; Yes shows "Data successfully restored." and the same notes. | not run | pass (second Linux user) |
| 13 | Wrong phrase | Change the phrase to another 16+ characters → **OK**. | An error about the sync phrase, and Settings open again. The server data are unchanged (check from the first device). | not run | pass (server revision unchanged) |
| 14 | Changed host key | Recreate the SSH container with new host keys; connect. **Settings** → **Reset** → **OK**; connect again. | The connection is refused with both fingerprints. After Reset the key is asked about again and the connection works. | not run | pass |
| 15 | Backup with deletions | Delete two notes → **Save**. | "Would you like to save data on remote storage?" with "Backup will delete 2 notes on the server."; Yes shows "Data successfully saved.". | not run | pass |
| 16 | Restore with deletions | On the second device add a note and Apply. On the first device change a note → **Save**. On the second device open **Settings** → **OK** (the connect check) and answer **No**, then press **Restore**. | "Would you like to restore data from remote storage?" with "Restore will delete 1 local notes."; Yes shows "Data successfully restored.". | not run | pass |
| 17 | Close with changes | Edit a note, Apply, close the window → **Yes**. Repeat with the SSH container stopped. | "Would you like to save your data changes to remote database?"; Yes saves, shows "Data successfully synchronized." and quits. With the server down the error is shown, then "Close anyway?": No keeps the window, Yes quits. | not run | pass (both branches) |
| 18 | Local only | Clear every field in Settings → **OK**. | No connection is attempted; Backup and Restore are inactive; notes still work. | not run | pass |
| 19 | Second instance | Start Smartnote while it runs. | The running window comes to the front; no second window, the storage is untouched. | not run | pass (second process exits; focus not observable under Xvfb) |
| 20 | Network break | Start a Backup and stop the SSH container during it (`docker pause`). | An error (timeout or SSH); Backup and Restore become inactive; no notes are lost, the next Backup works after the server is back. | not run | not run |

## Search scenarios

The scenarios of the search specification (section 12.6). Before scenario 21 create these notes with
**New** → **Apply changes** (paste the texts; use spaces, not tabs, in the code) and wait until the
status line under the hits no longer shows `Semantic search is loading` or `Indexing N of M`. A hit
shows the note name, `name` or `line N` (the lines of the text count from 1) and the line with the
match highlighted.

- **Блины**: `Рецепт блинов` / `Взбить три яйца с полулитром молока, добавить стакан муки, щепотку соли и ложку сахара.` / `Жарить на горячей сковороде с двух сторон до золотистого цвета.`
- **Garden**: `Tomato seedlings` / `Water the tomato seedlings in the greenhouse every morning.` / `Open the windows when the sun is out and close them at night.`
- **Встречи**: `Встречи с клиентами переносятся на четверг.` / `Подготовить отчёт о продажах за квартал.` / `Позвонить поставщику по поводу задержки.`
- **Running log**: `She runs five kilometres every day.` / `Prepare the quarterly sales report for the board.` / `The report on sales is due next quarter.`
- **Retry code**: a Go function of 10 lines: `func retryBackoff(attempt int) time.Duration {`, `if attempt == 0 {`, `return time.Second`, `}`, `delay := baseDelay << attempt`, `if (delay > maxDelay) {`, `return maxDelay`, `}`, `return delay`, `}`.
- **Journal**: 100 lines `Entry NN: routine check, nothing unusual today.`, except line 91 `Entry 91: the old lighthouse keeper sent a postcard from the coast.`; the text needs scrolling.

The model is off when its directory is missing: quit Smartnote and rename `usr/share/smartnote/model`
(`/usr/local/share/smartnote/model` for the `.deb`, `squashfs-root/usr/share/smartnote/model` for an
extracted AppImage) to `model.off`; rename it back afterwards. A CPU without AVX2 has the same effect.
The worker is the process `smartnote-search`; kill it with `kill -9 $(pidof smartnote-search)`
(`pkill -x` does not match the name, which the kernel cuts to 15 characters).

| # | Scenario | Steps | Expected | .deb | AppImage |
|---|---|---|---|---|---|
| 21 | Words, Russian morphology | `Ctrl+F`, type `встреча клиент`. | **Встречи** `line 1` («Встречи с клиентами переносятся на четверг.») first, then **Встречи** `name`: other word forms of both words match. Every matching word of a line is highlighted: «Встречи» and «клиентами» in `line 1`. | pass (rerun 2026-11-04 after the fix wave: both words highlighted, the jump selects «Встречи») | pass (rerun 2026-11-04, the same) |
| 22 | Words, English morphology | Search `running`. | **Running log** `name` and **Running log** `line 1` with `runs` highlighted. | pass | pass |
| 23 | Phrase | Search `"quarterly sales report"`, then the same words without quotes. | With quotes: only **Running log** `line 2`, the phrase highlighted. Without quotes: also `line 3` («The report on sales…»). | not run: the `.deb` run covered the core subset (21, 22, 24, 26–30, 33); the scenario does not depend on the package | pass |
| 24 | Substring in an identifier and in code | Search `ryBack`, then `<< attempt`. | `ryBack`: **Retry code** `line 1`, highlighted inside `retryBackoff`. `<< attempt`: **Retry code** `line 5` first with `<< attempt` highlighted; lines with the word `attempt` follow. Up to 3 lines found only by meaning may follow them (rev 4 allows it; here lines of **Journal**). | pass (`ryBack`, `==` with the model on) | pass |
| 25 | Operators with the model off | Turn the model off (see above), start and unlock. Search `==`, `(`, then `meeting with customers`. | The status line says `Semantic search unavailable`. `==`: **Retry code** `line 2`. `(`: **Retry code** `line 1` and `line 6`, the bracket highlighted. `meeting with customers`: `No matches`. `smartnote.log` has `semantic search unavailable: model files: …`. | not run: the `.deb` run covered the core subset (21, 22, 24, 26–30, 33); the scenario does not depend on the package | pass |
| 26 | Cross-lingual meaning | Model on. Search `meeting with customers`, press Enter. Then search `полив рассады помидоров в теплице` and `pancake recipe`. | `meeting with customers`: **Встречи** `line 1` without highlighted words (found by meaning only); Enter selects the whole fragment. Under spec rev 4 a line found only by meaning is shown when its score is at least 0.84, so most cross-lingual queries show `No matches`: on the reference set only 1 of 18 is found, and the other two queries here show `No matches`. That is the expected result, not a failure. | pass (`meeting with customers`) | pass |
| 27 | No matches | Search `qwertyzxcv`. | `No matches` under the field, no hits. | pass | pass |
| 28 | Jump to a line and to the name | Select **Garden** in the list. Search `lighthouse`, press Enter. Then search `Garden`, press Enter. | `lighthouse`: **Journal** opens and its text scrolls so that line 91 is visible; `lighthouse` is selected and the text has the focus. `Garden`: the note opens with `Garden` selected in the Name field. | pass (line; the jump to the name not run: the same frontend code, checked from the AppImage) | pass |
| 29 | Draft and a hit in the same note | Open **Garden**, add text to it, do not apply. Search `greenhouse`, press Enter; press Enter in the question. Open the hit again and press **Discard**. | `Discard unsaved changes?` with **Discard** and **Cancel** also for the note already open. Cancel has the focus: Enter keeps the draft and nothing moves. Discard shows the saved text with `greenhouse` selected. | pass | pass |
| 30 | New note with text, then a hit | **New**, type a name and a text, click another note in the list. Search `lighthouse`, press Enter; press Enter in the question. Open the hit again and press **Discard**. | During New the list is inactive and the click does nothing. The hit asks `Discard unsaved changes?`; **Cancel** has the focus, a deliberate deviation from v1, whose questions focus Yes (the other questions of Smartnote still do): Enter keeps the new note being written, with its name and text. Discard opens **Journal** with `lighthouse` selected. | pass | pass |
| 31 | Text before the match changed after the search | Search `lighthouse`, press Escape. Open **Journal**, insert two empty lines at the top, **Apply changes**. `Ctrl+F` (the old hit still says `line 91`), Enter. Then replace `lighthouse` in the text with another word, apply, open the old hit again. | The text of the hit is no longer at its place, so the search runs again: `lighthouse` on line 93 is selected and the hit now says `line 93`. When the word is gone, **Journal** opens at the top without a selection and the hits show `No matches`. | not run: the `.deb` run covered the core subset (21, 22, 24, 26–30, 33); the scenario does not depend on the package | pass (both variants) |
| 32 | Restore | Connect to the empty server of this section's header as in scenarios 10–11: **Settings**, SSH host and port of the OpenSSH container (`22`), user `tester`, password `testpass`; database host `127.0.0.1`, port `5432`, database `smartnote`, user and password of the `postgres:17-alpine` container (`POSTGRES_USER`, `POSTGRES_PASSWORD`); a sync phrase of 16 or more characters; **Test connection**, trust the key, **OK**: the first Backup runs. Then change `greenhouse` to `glasshouse` in **Garden** and add a note with `zeppelin`, apply both. Search both words. Open **Settings** and press **OK** without changes (the connect check; it makes **Restore** active), then **Restore** → **Yes**. Search `glasshouse`, `zeppelin`, `greenhouse`, `meeting with customers`. | Before Restore both words are found. "Would you like to restore data from remote storage?" with "Restore will delete 1 local notes."; "Data successfully restored.". After it `glasshouse` and `zeppelin` show `No matches`, `greenhouse` finds **Garden** `line 2` and the cross-lingual hit is back: the index follows the restored notes. | not run: the server part ran from the AppImage only, as the plan of task 17b set; the index code is the same in both packages | pass |
| 33 | Worker crash | Kill the worker (see above) and search at once; search again after 2 s. Kill it and save a note change at once, then search the new text. Kill each new worker as soon as it starts until Smartnote gives up; then search and edit a note. | Notes, the list and Apply keep working; a save does not wait for the worker. While no worker runs (from 1 s up to 16 s) the status line shows `Search unavailable` and a query shows no hits, never `No matches`. When the worker is back, the current query runs again by itself and shows its hits, including the change saved meanwhile. The restarts come after 1, 2, 4, 8 and 16 s; after 5 failed restarts within 10 minutes (the sixth failure) Smartnote gives up: `Search unavailable`, `smartnote.log` says the search is unavailable until the next start, notes still work. | pass (rerun 2026-11-04 after the fix wave, all steps: `Search unavailable` while down, the query came back by itself 4 times of 4, the change saved meanwhile found, gave up at the sixth failure) | pass (rerun 2026-11-04, all steps; 3 of 3 kills recovered by themselves) |

## Results

Tested by: Claude Code (scripted run) Date: 2026-10-31 Package versions: branch `v2-15-ci-docs`, AppImage built by `task package:linux`

How: the AppImage in a clean `ubuntu:26.04` container under Xvfb with openbox, driven by `xdotool`,
checked on screenshots; the server was the OpenSSH test image and `postgres:17-alpine` sharing its
network namespace. The `.deb` was installed in a clean `ubuntu:26.04` container and checked with
scenario 1 only. Scenario 20 was not run; the break during Backup
is covered by the integration test `TestBackupCommitAckLost` and the use case tests (Review Focus 3).

Defects found and fixed:
- Closing with a failed Backup showed the raw host key mismatch text; fixed so that it shows both
  fingerprints as the connect check does.

Notes:
- The trust question shows the key the server offers first, with OpenSSH defaults the ECDSA key;
  README now says to compare with every host key.
- WebKitGTK keeps its own `storage` and `mediakeys` directories in the data directory
  (`~/.local/share/smartnote`); the app stores nothing there.
- After the PR review, rechecked with the AppImage under Xvfb: two starts at the same moment leave
  exactly one Smartnote running (8 rounds); a start that fails (data directory path is a file)
  writes the error to `smartnote.log`; Settings takes the keyboard focus when it opens, and Tab
  does not reach the Main screen behind it.

Tested by: Claude Code (scripted run) Date: 2026-11-04 Package versions: search scenarios 21–33,
branch `search-17-quality` at `965c7f2`, `smartnote.deb` 2.0.1-1 and `smartnote-x86_64.AppImage` built by
`task package:linux` (worker 0.1.0, model `e5s-ru-en-q8`, ONNX Runtime 1.31.0)

How: each package in its own `ubuntu:26.04` container under Xvfb with openbox, driven by `xdotool`
and checked on screenshots, the app running as a non-root user with
`WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1` (bwrap cannot run in the container; test stand only).
The `.deb` was installed with `apt install`; a second, untouched `ubuntu:26.04` container installed
it as well and pulled in its dependencies, with nothing missing for `ldd`. The AppImage was run
extracted (`--appimage-extract`). The notes were created through the interface. Kernel 7.0.0: the
worker reported `sandbox=full` from both packages (`searchsmoke` against the installed files), and
the running worker had `NoNewPrivs: 1`, `Seccomp: 2` and only descriptors 0, 1, 2 open. All scenarios
ran from the AppImage; from the `.deb` the core subset (21, 22, 24, 26–30, 33). Restore (32) used
the OpenSSH test image and `postgres:17-alpine` sharing its network namespace, from the AppImage only.
`smartnote.log` of both runs holds no note text and no query.

Defects found (fixed in the final fix wave, rechecked below):
- While the worker restarts (scenario 33) a query showed `No matches` rather than a status, and the
  query was not repeated when the worker was back; the user had to type again. Fixed: searches
  without a worker fail with `Search unavailable`, the status line says so during the restart, and
  the search bar runs the query again when the worker is ready
  (`fix(search): report unavailable search instead of no matches`).
- A hit highlighted one range, the first word found (scenario 21), not every query word in the line.
  Fixed: every word, phrase and substring match of the line is marked
  (`fix(search): highlight every match in a hit`).

Rerun of scenarios 21 and 33 after the final fix wave: Claude Code (scripted run), 2026-11-04, branch
`search-17-quality` at `164aa31`, `smartnote.deb` 2.0.1-1 and `smartnote-x86_64.AppImage` built by
`task package:linux`, each on a new storage in its own `ubuntu:26.04` container, stand as above. 21:
both query words are highlighted in `line 1`, Enter selects the first one. 33: right after the kill
the status line says `Search unavailable` with no hits; 1–7 s later the same query shows its hits
without typing again; a note saved while the worker was down is found after the restart; the kills
at +0, +4, +12 and +28 s (after the two of the earlier steps) end with "given up" in `smartnote.log`
at the sixth failure, `Search unavailable`, and the next Apply works. The worker after a restart has
`NoNewPrivs: 1` and `Seccomp: 2`. `smartnote.log` of both runs: failures and the given-up line only,
no note text, no query, no stderr line of the worker. The first rerun from the `.deb` found that the
query repeated on ready could still show `No matches` while the new worker's index was being
rebuilt; fixed (`fix(search): hold the search status until a rebuild is applied`) before this run.

Notes:
- A line found only by meaning has no highlight in the list; the jump selects its whole fragment.
- Lines found only by meaning still come up for some code queries (`<< attempt` and `attempt` show
  up to 3 lines of the repetitive **Journal** below the real hits), within the rev 4 limit of 3.
- Cross-lingual search finds `meeting with customers` → **Встречи**; `полив рассады помидоров в
  теплице` and `pancake recipe` show `No matches`, as rev 4 expects (score below 0.84).
