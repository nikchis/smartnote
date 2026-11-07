[English](smoke-windows-macos.md) | **Русский**

# Дымовой тест на Windows и macOS

Чек-лист для неподписанных пакетов Windows и macOS, включая проверки поиска, которые CI выполнить
не может. Для тестировщиков с доступом к этим системам.

Пакеты для Windows (`smartnote-amd64.msi`) и macOS (`smartnote.dmg`, только Apple
Silicon) собирает CI (задание `package`, артефакты `smartnote-windows` и `smartnote-darwin`), и они
не подписаны. Эта проверка выполняется вручную на каждой системе; столбцы **Результат** и
**Проверил** заполняет тестировщик.

Сервер — тот же, что в [ручной приёмке](manual-test.ru.md) (контейнеры OpenSSH и PostgreSQL),
доступный с тестовой машины.

Шаги поиска проверяют то, чего не может CI: на Windows и macOS поисковый воркер `smartnote-search`
работает без песочницы Linux, поэтому тестировщик подтверждает, что воркер завершается вместе с
приложением, не открывает сетевых соединений и что ONNX Runtime не пишет файлы телеметрии
(`onnxruntime.db`, `deviceid`) в профиль пользователя (спецификация, разделы 6.1 и 14). Запишите
время перед первым запуском Smartnote: более старые файлы не относятся к этой проверке.

## Windows 10/11

| № | Шаг | Ожидается | Результат | Проверил |
|---|---|---|---|---|
| 1 | Запустите `.msi`; на «Windows protected your PC» выберите **More info** → **Run anyway**. | Запроса прав администратора нет; установка завершается; Smartnote есть в меню «Пуск» и в `%LOCALAPPDATA%\Programs\Smartnote`. | | |
| 2 | Запустите Smartnote. | Экран New storage открывается в тёмных цветах v1 — и при светлой, и при тёмной теме системы. Логотип Smartnote есть у `smartnote.exe` в Проводнике, в заголовке окна и на панели задач. | | |
| 3 | Создайте хранилище, добавьте заметку. | «Note has been successfully added.»; `local.db` лежит в `%APPDATA%\smartnote`. | | |
| 4 | **Settings**: создайте ключ, добавьте открытый ключ на сервер, заполните настройки сервера, **Test connection**, доверьтесь ключу. | «Connection successfully established.» (или «… Remote storage is empty.»). | | |
| 5 | **OK**, измените заметку, **Backup**. | «Data successfully saved.» | | |
| 6 | Войдите вторым пользователем Windows, установите или запустите Smartnote, создайте хранилище, настройте тот же сервер и фразу. | Вопрос о Restore; Yes показывает «Data successfully restored.» и заметки первого пользователя. | | |
| 7 | Измените заметку, закройте окно. | «Would you like to save your data changes to remote storage?»; Yes показывает «Data successfully synchronized.», и приложение закрывается. | | |
| 8 | Запустите Smartnote дважды. | Второй запуск выводит открытое окно на передний план. | | |
| 9 | Измените заметку, выйдите из Windows с открытым Smartnote. | Windows выходит, не дожидаясь Smartnote; после входа заметка на месте. | | |
| 10 | Добавьте заметку «Pancakes» с текстом «flour, milk, eggs». Нажмите Ctrl+F, введите `milk`, затем `recipe for crepes`. | `smartnote-search.exe` и `onnxruntime.dll` лежат в папке установки вместе с папкой `model`. Не появляется ни «Search unavailable», ни, через несколько секунд после запуска, «Semantic search unavailable»; оба запроса показывают «Pancakes», и Enter на результате переходит к его строке. | | |
| 11 | При открытом Smartnote в PowerShell: `Get-NetTCPConnection -OwningProcess (Get-Process smartnote-search).Id; Get-NetUDPEndpoint -OwningProcess (Get-Process smartnote-search).Id`. | `smartnote-search` работает; обе команды ничего не выводят (ошибка «No matching MSFT_NetTCPConnection objects» — ожидаемый пустой результат). | | |
| 12 | Закройте Smartnote, затем в PowerShell: `Get-Process smartnote-search`. | Такого процесса нет. | | |
| 13 | В PowerShell: `Get-ChildItem $env:USERPROFILE -Recurse -Force -ErrorAction SilentlyContinue -Include onnxruntime.db, deviceid* \| Select-Object FullName, LastWriteTime`. | Ничего не записано после начала проверки. | | |
| 14 | На чистой Windows без Microsoft Visual C++ Redistributable (x64) (**Settings → Apps** не показывает «Microsoft Visual C++ 2015-2022 Redistributable (x64)»; новая виртуальная машина) установите и запустите Smartnote, добавьте заметку из шага 10, найдите `milk`; посмотрите `%LOCALAPPDATA%\smartnote\smartnote.log`. Затем установите Redistributable (`vc_redist.x64.exe` от Microsoft), перезапустите Smartnote и найдите `recipe for crepes`. | Без Redistributable: `milk` показывает «Pancakes» (полнотекстовый поиск работает), показывается «Semantic search unavailable», а в журнале причина «semantic search unavailable: loading the ONNX Runtime library failed». После установки: «Semantic search unavailable» нет, когда модель загрузилась, и `recipe for crepes` показывает «Pancakes». | | |

## macOS

| № | Шаг | Ожидается | Результат | Проверил |
|---|---|---|---|---|
| 1 | Откройте `.dmg`, перетащите Smartnote в Applications, запустите; когда Gatekeeper откажет, откройте **System Settings → Privacy & Security** → **Open Anyway** (или `xattr -dr com.apple.quarantine /Applications/smartnote.app`). | Smartnote запускается; логотип Smartnote есть в Dock. | | |
| 2 | Создайте хранилище, добавьте заметку. | «Note has been successfully added.»; `local.db` лежит в `~/Library/Application Support/smartnote`. | | |
| 3 | **Settings**: создайте ключ, добавьте открытый ключ на сервер, заполните настройки сервера, **Test connection**, доверьтесь ключу. | «Connection successfully established.» | | |
| 4 | **OK**, измените заметку, **Backup**. | «Data successfully saved.» | | |
| 5 | Войдите вторым пользователем macOS, запустите Smartnote, создайте хранилище, настройте тот же сервер и фразу. | Вопрос о Restore; Yes показывает «Data successfully restored.» | | |
| 6 | Измените заметку, закройте приложение через Cmd+Q. | Перед закрытием появляется вопрос о сохранении; после Yes — «Data successfully synchronized.» | | |
| 7 | Снова запустите Smartnote из Applications, пока он работает. | Открытое окно выходит на передний план. | | |
| 8 | Измените заметку, **Apple menu → Log Out** с открытым Smartnote. | macOS выходит из системы без вопроса о сохранении и без «Smartnote canceled logout»; после входа заметка на месте. | | |
| 9 | `codesign --verify --deep --strict --verbose=2 /Applications/smartnote.app` | «valid on disk» и «satisfies its Designated Requirement». | | |
| 10 | Добавьте заметку «Pancakes» с текстом «flour, milk, eggs». Нажмите Cmd+F, введите `milk`, затем `recipe for crepes`. | `smartnote-search` и `libonnxruntime.1.31.0.dylib` лежат в `smartnote.app/Contents/MacOS`, модель — в `Contents/Resources/model`. Не появляется ни «Search unavailable», ни, через несколько секунд после запуска, «Semantic search unavailable»; оба запроса показывают «Pancakes», и Enter на результате переходит к его строке. | | |
| 11 | При открытом Smartnote в Terminal: `lsof -nP -i -a -p "$(pgrep -x smartnote-search)"`. | `pgrep` находит воркер; `lsof` не показывает соединений. | | |
| 12 | Закройте Smartnote через Cmd+Q, затем `pgrep -lx smartnote-search`. | Вывода нет. | | |
| 13 | `find ~ \( -name onnxruntime.db -o -name 'deviceid*' \) -newermt "<start time>" 2>/dev/null` со временем, записанным перед проверкой, например `"2026-11-05 10:00"`. | Вывода нет. | | |
