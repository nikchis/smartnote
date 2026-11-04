[English](packaging.md) | **Русский**

# Упаковка

Что входит в пакеты для Linux, Windows и macOS и как их собирают задачи упаковки: метаданные и
значок, поисковый воркер с моделью и библиотекой ONNX Runtime, лицензионные уведомления и подпись
на macOS. Для разработчиков, которые собирают или меняют пакеты; команды описаны в разделе
[Сборка из исходников](building.ru.md).

## Метаданные и значок

- Метаданные пакетов (имя, поставщик, версия, авторские права, описание) хранятся в
  `build/config.yml`; `wails3 task common:update:build-assets` копирует их в
  `build/darwin/*.plist`, `build/windows/info.json`, `build/windows/nsis/wails_tools.nsh`,
  `build/linux/desktop` и `build/linux/nfpm/nfpm.yaml`. Эта задача переписывает `nfpm.yaml` из
  шаблона Wails и сбрасывает `maintainer` (в `${GIT_COMMITTER_NAME} <${GIT_COMMITTER_EMAIL}>`,
  пустое в CI) и `homepage` (в `https://wails.io`): после её запуска верните их. `build/windows/msix/*.xml`
  не генерируется и поддерживается вручную. Задача также создаёт `build/ios/`, который проекту не
  нужен и не хранится.
- Исходник значка — `build/appicon.svg`; `build/appicon.png` — его отрисовка 1024×1024, и
  `common:generate:icons` строит из этого PNG `windows/icon.ico` и `darwin/icons.icns`.
  `Assets.car` из Icon Composer нет: на macOS с actool 26 Wails пересобрал бы его, а вместе с ним
  и `icons.icns`, из `appicon.icon`, поэтому macOS использует `icons.icns` (`CFBundleIconFile`).

## Пакеты с поиском

Начиная с выпуска 2.1.0 (задача 16 плана) каждый пакет содержит поисковый воркер
`smartnote-search`, официальную библиотеку ONNX Runtime 1.31.0 и файлы модели `model.onnx`,
`tokenizer.kit` и `manifest.json` по путям из спецификации, раздел 8.2 (`tokenizer.json` нужен
только тестам и в пакеты не входит), а также лицензионные уведомления: `THIRD_PARTY_NOTICES.md` с
`licenses/onnxruntime/{LICENSE,ThirdPartyNotices.txt}`, `licenses/rust-crates.txt` и
`licenses/go-modules.txt`.

- `task search:files` готовит эти файлы и является зависимостью `linux:create:appimage`,
  `linux:create:deb` (и `create:rpm`, `create:aur`), `darwin:create:app:bundle` и
  `windows:create:nsis:installer` (установщика из `windows:package`), поэтому `task package:<os>`
  работает из чистой копии репозитория. Она запускает `search:build`, `model:fetch` и `ort:fetch`;
  `notices:build` задачи упаковки запускают сами, когда приложение уже собрано, потому что
  `go list` нужен `frontend/dist` (`go:embed`). Каждая из них выполняется один раз за вызов `task`.
- `task search:build` запускает `tools/searchbuild`: `cargo build --locked --release -p worker` для
  целевой платформы Rust, соответствующей `GOOS`/`ARCH` (`linux/amd64` → `x86_64-unknown-linux-gnu`,
  `darwin/arm64` → `aarch64-apple-darwin`, `darwin/amd64` → `x86_64-apple-darwin`, `windows/amd64` →
  `x86_64-pc-windows-msvc`), и копирует исполняемый файл, названный в JSON-сообщениях cargo, в
  `bin/smartnote-search[.exe]`, так что `CARGO_TARGET_DIR` и `RUSTUP_TOOLCHAIN` из окружения
  учитываются. Cargo запускается каждый раз. `GOOS`, отличный от системы сборки, или платформа без
  воркера останавливают задачу с сообщением: воркер не собирается кросс-компиляцией. На Windows
  воркер компонуется со статической средой выполнения C (`search/.cargo/config.toml`,
  `-C target-feature=+crt-static`; переменная `RUSTFLAGS` заменила бы этот флаг): установщик не
  содержит Visual C++ Redistributable. Официальная `onnxruntime.dll` всё же импортирует
  `MSVCP140.dll`, `MSVCP140_1.dll`, `VCRUNTIME140.dll` и `VCRUNTIME140_1.dll`; без Redistributable
  она не загружается, семантический поиск недоступен («semantic search unavailable: loading the
  ONNX Runtime library failed» в журнале), а полнотекстовый поиск работает (это проверяет
  `ort_load_failure_keeps_full_text_search` в `tests/e2e.rs` со сломанной зависимостью библиотеки на
  Linux). Эти DLL в установщик не копируются: это были бы незакреплённые двоичные файлы.
- `task ort:fetch` загружает библиотеку для `GOOS`/`ARCH` и извлекает из архива выпуска ещё
  `LICENSE` и `ThirdPartyNotices.txt` (`notices` из `ort.lock.json`; SHA-256 архива покрывает и их,
  повторный запуск только проверяет, что они на месте) в `build/ort`. Платформа без закреплённой
  библиотеки (darwin/amd64, linux/arm64) останавливается здесь с сообщением; так же
  останавливаются `darwin:package:universal` (воркер и библиотека есть только для arm64) и пакет
  MSIX (`FORMAT=msix`, в нём не было бы файлов поиска).
- `task notices:build` запускает `tools/notices`: крейты, которые воркер компонует для своей
  платформы, по `cargo metadata` (обычные зависимости пакета `worker`, без dev- и
  build-зависимостей), и модули Go из `go list -deps -tags production ./cmd/smartnote` для целевой
  системы, каждый с файлами лицензий своего исходного пакета (`LICENSE*`, `COPYING*`, `NOTICE*` и
  подобные; одинаковые тексты записываются один раз). Крейты без файлов лицензий: крейты tantivy
  покрыты файлами `tantivy` (тот же репозиторий); `kitoken` — текстом BSD-2-Clause из его
  репозитория на v0.11.1, который хранится в `tools/notices/extra/kitoken/LICENSE`; у `htmlescape`
  0.3.1 (Apache-2.0 / MIT / MPL-2.0) лицензии нет ни в крейте, ни в репозитории, поэтому
  `extra/htmlescape/LICENSE-MIT` — это лицензия MIT с автором из его `Cargo.toml` в качестве
  правообладателя. Стандартная библиотека Rust указывается с версией `rustc` и файлами
  `LICENSE-MIT` и `LICENSE-APACHE` из rust-lang/rust на 1.99.0 (`extra/rust-std`; копии в
  `share/doc/rust/licenses` набора инструментов — шаблоны без строки об авторских правах),
  стандартная библиотека Go — с `$GOROOT/LICENSE`. Пакет совсем без текста лицензии называется в
  предупреждении.
- `.deb`: `contents` из `nfpm.yaml`, воркер 0755, остальные файлы 0644; уведомления ложатся в
  `/usr/share/doc/smartnote/`. `task package:linux` заканчивается `task check:deb`
  (`build/linux/check-deb.sh`, по выводу `dpkg-deb -c`).
- AppImage: `wails3 generate appimage` кладёт в AppDir только приложение и запускает на нём
  linuxdeploy. Повторный запуск linuxdeploy после добавления файлов развернул бы и их и выставил бы
  RPATH библиотеке ONNX Runtime, что меняет файл и ломает его закреплённый SHA-256. Поэтому файлы
  добавляются в AppDir, который Wails оставляет в `build/linux/appimage/build`, и AppDir заново
  упаковывает `linuxdeploy-plugin-appimage` из того же AppImage linuxdeploy (распакованного через
  `--appimage-extract`), который только упаковывает. AppRun от Wails сохраняется.
- Windows: `project.nsi` устанавливает файлы рядом с `smartnote.exe` (`model\`, `licenses\`), а
  деинсталлятор удаляет их перед удалением папки.

Файлы модели готовятся и публикуются так, как описано в
[tools/model/README.md](../tools/model/README.md); файл уведомлений —
[THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md).

## Пакет macOS и подпись

Файлы копируются в пакет приложения до `codesign:adhoc`. Библиотека ONNX Runtime от Microsoft
подписана (Developer ID, блок CMS в подписи); `codesign --force --deep` для всего пакета подписал
бы её заново и изменил бы файл, и воркер бы её отверг. Поэтому воркер подписывается отдельно, пакет —
без `--deep`, `codesign --verify --deep --strict` проверяет всё целиком, а `cmp` — что библиотека в
пакете совпадает с загруженной.

Ловушка: `task darwin:sign` и `darwin:sign:notarize` запускают `wails3 tool sign`, который
подписывает с `codesign --deep --force`: он подписал бы библиотеку заново и молча отключил бы
семантический поиск. Подписи Developer ID нужен тот же порядок, что и у `codesign:adhoc`, а
hardened runtime, которого требует нотаризация, включает проверку библиотек: тогда загрузка
воркером библиотеки, подписанной другой командой (Microsoft), требует решения — например,
entitlement `com.apple.security.cs.disable-library-validation` для воркера или копии, подписанной
нашей собственной подписью, чей SHA-256 пришлось бы закрепить вместо нынешнего.

## Дымовая проверка AppImage

`task smoke:appimage` собирает `cmd/searchsmoke` статически (`CGO_ENABLED=0`) в `build/smoke`;
после проверки интерфейса ([заметки разработчика, проверка 3](development.ru.md#проверка-3-пакеты-и-appimage-в-чистом-контейнере))
`build/linux/smoke-appimage.sh` распаковывает AppImage от непривилегированного пользователя и
запускает его с воркером, библиотекой и моделью из распакованного образа, из `usr/lib` и с тем
`LD_LIBRARY_PATH`, который AppRun передаёт приложению (адаптер передаёт его дальше воркеру).
Результат в `ubuntu:26.04`: `ready worker=0.1.0 lexical=ready semantic=ready sandbox=full`,
`hits=1`. AppImage не содержит `libstdc++`; ONNX Runtime загружает системную под песочницей
воркера. Запуск с AppImage версии v2.0.1 проверку не проходит (`no such file or directory` для
воркера).

## Размеры и CI

Размеры пакетов без файлов поиска и с ними (2026-11-04): `.deb` 8,8 МБ → 134,8 МБ, AppImage
135,9 МБ → 254,5 МБ.

Задание CI `package` ставит набор инструментов из `search/rust-toolchain.toml`, кэширует cargo
(`Swatinem/rust-cache`), `build/model` и `build/ort` с ключами задания `search`, затем запускает
`task package:<os>`. На macOS и Windows оно запускает `cmd/searchsmoke` против упакованного воркера
(на macOS — в подписанном пакете) с `ORT_DISABLE_TELEMETRY=1`. Выпуск модели `model-e5s-1` должен
быть опубликован, прежде чем `task model:fetch` заработает в CI; локально `MODEL_BASE_URL=file:///…/`
направляет его в каталог.
