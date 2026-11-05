[English](install.md) | **Русский**

# Установка и обновление

Как установить Smartnote на Linux, Windows и macOS, что нужно от системы семантическому поиску,
как обновиться с 2.0.x и где приложение хранит свои файлы. Для пользователей.

Пакеты — на [странице выпусков](https://github.com/nikchis/smartnote/releases). Они не подписаны,
поэтому Windows и macOS предупреждают перед первым запуском.

## Ubuntu (.deb)

Ubuntu 26.04, amd64, файл `smartnote_<версия>_amd64.deb`:

```
sudo apt install ./smartnote_<версия>_amd64.deb
```

Команда подтягивает GTK 4 и WebKitGTK 6.0.

## Ubuntu (AppImage)

Ubuntu desktop, amd64, файл `Smartnote-<версия>-x86_64.AppImage`: сделайте его исполняемым (`chmod +x`) и
запустите. Минимальной системе нужны ещё `libegl1 libgles2 libfontconfig1 libfribidi0
libharfbuzz0b bubblewrap xdg-dbus-proxy`; в любой настольной Ubuntu они есть.

## Windows

Windows 10/11, файл `Smartnote-<версия>-windows-amd64-installer.exe`. SmartScreen показывает «Windows protected your
PC»: выберите **More info**, затем **Run anyway**.

Семантическому поиску нужен Microsoft Visual C++ Redistributable (x64), который есть в большинстве
систем; без него работает только полнотекстовый поиск.

## macOS

macOS 12+ на Apple Silicon (arm64), файл `Smartnote-<версия>-macos-arm64.dmg`. Пакета для Mac на Intel нет: библиотека
ONNX Runtime для поиска закреплена только для Apple Silicon ([Упаковка](packaging.ru.md)).

Gatekeeper не открывает неподписанное приложение: перетащите Smartnote в Applications, затем либо
после первой попытки откройте **System Settings → Privacy & Security** и нажмите **Open Anyway**,
либо выполните

```
xattr -dr com.apple.quarantine /Applications/smartnote.app
```

## Требования семантического поиска

На компьютерах x86-64 семантическому поиску нужен процессор с AVX2, на Windows — ещё и Visual C++
Redistributable (см. выше). Без них Smartnote работает, полнотекстовый поиск тоже, а строка
состояния поиска показывает `Semantic search unavailable` ([Поиск](search.ru.md#строка-состояния)).

## Обновление с 2.0.x

2.1.0 добавляет локальный поиск и хранит локальное хранилище в формате 2, где есть зашифрованные
поисковые данные заметок. Первая разблокировка в 2.1.0 окончательно преобразует `local.db`, и 2.0.x
после этого отказывается его открывать с сообщением «Data was created by a newer version of
Smartnote.». Обратного преобразования нет.

Чтобы вернуться на 2.0.x:

1. Сохраните заметки на свой сервер в 2.1.0 (**Save**).
2. В 2.0.x начните с нового локального хранилища (уберите `local.db` в другое место).
3. Заново настройте подключение в **Settings**. SSH-ключ тоже хранится в `local.db`: импортируйте
   файл своего ключа или создайте новый ключ и разрешите его на сервере.
4. **Restore**.

## Где лежат данные

| Система | Локальное хранилище (`local.db`) | Журнал ошибок (`smartnote.log`) |
|---|---|---|
| Linux | `$XDG_DATA_HOME/smartnote` (`~/.local/share/smartnote`) | `$XDG_STATE_HOME/smartnote` (`~/.local/state/smartnote`) |
| Windows | `%APPDATA%\smartnote` | `%LOCALAPPDATA%\smartnote` |
| macOS | `~/Library/Application Support/smartnote` | `~/Library/Logs/smartnote` |

В журнале только ошибки — без паролей, фразы синхронизации, ключей, названий и текстов заметок и
строки подключения.

На Linux логотип для заголовка окна записывается в `$XDG_CACHE_HOME/smartnote/icons`
(`~/.cache/smartnote/icons`), потому что GTK 4 берёт значки окон только из темы значков; больше там
ничего нет, а после удаления файл записывается снова.

## Интеграция с рабочим столом

- Одновременно работает только один Smartnote: повторный запуск выводит открытое окно на передний
  план.
- Идентификатор приложения — `com.nikchis.smartnote`; `.deb`, `.rpm` и AppImage несут ярлык
  `com.nikchis.smartnote.desktop` под этим именем, поэтому рабочие столы на Wayland показывают
  логотип у окна, и метаданные AppStream для центров приложений.
- Интерфейс использует шрифт Liberation Sans (`fonts-liberation`), заметки — DejaVu Sans Mono 13pt
  (`fonts-dejavu-core`); оба — зависимости пакетов.
- Цвета — тёмная тема v1, какую бы тему ни предпочитала система.

Дальше: [Настройка сервера](server-setup.ru.md), если заметки нужно сохранять на свой сервер.
