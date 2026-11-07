[English](building.md) | **Русский**

# Сборка из исходников

Инструменты, системные пакеты и команды, нужные для сборки, тестирования и упаковки Smartnote; для
разработчиков.

## Инструменты

| Инструмент | Версия |
|---|---|
| Go | 1.27.3 (`toolchain` в `go.mod`) |
| Node.js / npm | LTS (разработка велась на 24.21.0 / 12.0.2) |
| Rust | 1.99.0 с rustfmt и clippy (`search/rust-toolchain.toml`) |
| Wails CLI | v3.1.0: `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0` |
| Task | 3.54.0: `go install github.com/go-task/task/v3/cmd/task@v3.54.0` |
| golangci-lint | 2.15.0: `make install-lint` кладёт его в `bin/` |

В `go.mod` записано `go 1.26.0`, а не `go 1.26`: так его переписывает `go mod tidy`, потому что сам
Wails v3 требует `go 1.26.0`; оба варианта означают одну версию языка.

На Ubuntu 26.04 для сборки нужны ещё `build-essential pkg-config libgtk-4-dev
libwebkitgtk-6.0-dev` (`wails3 doctor` перечисляет, чего не хватает; разработка велась с GTK 4.22.4
и WebKitGTK 2.52.6). Для пакета Windows нужен WiX Toolset v6 (`dotnet tool install --global wix
--version 6.0.2`).

Некоторым командам нужно больше: Docker — для `make integration` и `task smoke:appimage`; `gdb` и
`strace` — для `make search-test`; `cargo-deny` — для `make search-lint`; ночной набор
инструментов Rust — для `make search-asan`; [uv](https://docs.astral.sh/uv/) — для
`task model:build`.

## Команды

Повседневные проверки запускаются через `make` (`make help` перечисляет цели); сборки и пакеты
Wails — через Task, который вызывают `make build`, `make package` и `make smoke`.

| Команда | Назначение |
|---|---|
| `make all` | `dep`, `test` и `lint` |
| `make test` | модульные тесты Go с детектором гонок и общим покрытием |
| `make integration` | интеграционные тесты с PostgreSQL и OpenSSH в Docker |
| `make lint` | golangci-lint (ставится в `bin/` закреплённой версии) и govulncheck |
| `make fmt`, `make lint-fix` | отформатировать код, применить автоматические исправления линтера |
| `make search-test` | тесты поискового воркера на Rust с пробными программами; `SEARCH_MODEL=1` добавляет тесты модели |
| `make search-lint` | rustfmt, clippy и политика `cargo-deny` поискового воркера |
| `make search-bench` | качество и производительность поиска на эталонном наборе (нужна модель; занимает минуты) |
| `make search-asan` | небезопасный код воркера под AddressSanitizer ([Сопровождение поиска](search-maintenance.ru.md)) |
| `npm --prefix frontend run test` | модульные тесты интерфейса (Vitest) |
| `npm --prefix frontend run check`, `npm --prefix frontend run lint` | svelte-check и ESLint |
| `task dev` | запуск с живой перезагрузкой |
| `task build` | собрать `bin/smartnote` |
| `task search:files` | собрать поисковый воркер и загрузить модель и библиотеку ONNX Runtime ([Упаковка](packaging.ru.md)) |
| `task package:linux` | `.deb` и AppImage в `bin/` |
| `task package:windows` | MSI-пакет для одного пользователя в `bin/` (на Windows) |
| `task package:darwin` | `.app` и `.dmg` в `bin/` (на macOS) |
| `task smoke:appimage` | запустить AppImage в чистом контейнере `ubuntu:26.04` (нужен Docker) |

Ручные проверки пакетов описаны в документах [Ручная приёмка](manual-test.ru.md) и
[Дымовой тест на Windows и macOS](smoke-windows-macos.ru.md).
