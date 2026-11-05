[English](development.md) | **Русский**

# Заметки разработчика

Как устроен код приложения на Wails v3 и что показали три проверки рисков поставки в начале работы
над v2 (задача 1 плана, 2026-10-30, Ubuntu 26.04, amd64). Для разработчиков, которые меняют
приложение; инструменты и команды описаны в разделе [Сборка из исходников](building.ru.md), пакеты —
в разделе [Упаковка](packaging.ru.md).

## Устройство

- Точка входа — `cmd/smartnote/main.go`. `//go:embed` не может обратиться к родительскому каталогу,
  поэтому собранный интерфейс встраивает пакет `frontend` (`frontend/embed.go`, `frontend.Dist`),
  а `main` отдаёт `fs.Sub(frontend.Dist, "dist")`.
- `frontend/dist` должен существовать до компиляции кода Go, импортирующего `frontend`; `make test`,
  `make lint` и `make integration` сначала собирают фронтенд (`task common:build:frontend`).
- `Makefile` закрепляет golangci-lint в `bin/` и запускает `govulncheck` как `tool` из `go.mod`;
  `.golangci.yml` проверяет интеграционные тесты с тегом `integration`.
- В `go.mod` есть `ignore ./frontend/node_modules`, поэтому `./...` никогда не обходит пакеты npm
  (некоторые из них содержат файлы `.go`).
- Корневой `Taskfile.yml` определяет `MAIN_PKG: ./cmd/smartnote`; Taskfile платформ в `build/`
  передают его в `go build` и в `wails3 generate bindings`.
- Задачи серверного режима из шаблона (`build:server`, `run:server`, `build:docker`, `run:docker`)
  и `build/docker/Dockerfile.server` удалены: серверный режим Wails отдаёт привязанные сервисы по
  HTTP без аутентификации и проверки Origin, а в спецификации такого режима нет. Неиспользуемый
  `build/linux/appimage/build.sh` тоже удалён; AppImage собирает `wails3 generate appimage`.

## Проверка 1: привязки и ошибки

Был привязан временный сервис с `Echo(s string, n int64) (string, error)` и `Fail() error`;
`wails3 generate bindings -ts -i ./cmd/smartnote` выдал:

```ts
export function Echo(s: string, n: number): $CancellablePromise<string>
export function Fail(): $CancellablePromise<void>
```

Упакованное приложение (AppImage в чистом контейнере, см. проверку 3) вызвало обе функции и
сообщило:

```json
{"echoed":"ok:42","echoedType":"string","failThrew":true,"name":"RuntimeError","message":"probe failure text","cause":{}}
```

Итак, `int64` становится в TS `number`, `string` — `string`, а возвращённая ошибка Go отклоняет
промис с `Error`, у которого `message` равно `err.Error()`. Исходники Wails v3.0.0
(`pkg/application/bindings.go`) показывают, что отклонение — это `CallError{message, cause, kind}`:
`kind` равен `RuntimeError`, а `cause` — `json.Marshal(&err)` (или `ServiceOptions.MarshalError`
сервиса). Простое значение `errors.New` сериализуется в `{}`; тип ошибки с экспортируемыми полями
JSON приходит в `cause` этим объектом. Поэтому `wailsui` может отправлять `{code, message, ...}`
как само значение ошибки, а интерфейс читает его из `e.cause`.

Временный сервис удалён.

## Проверка 2: Content Security Policy

`frontend/index.html` содержит политику из спецификации, раздел 7.3:

```
default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'
```

`base-uri 'none'` и `form-action 'none'` добавлены после описанной ниже проверки, по итогам
ревью кода задач 1–3: на странице нет `<base>` и нет формы, которая отправляется переходом.

Временный обработчик `securitypolicyviolation` сообщал в Go о каждом нарушении вместе с вычисленным
цветом элемента, стилизованного блоком `<style>` компонента Svelte. Для контроля страница добавляла
`<img>` с `https://example.invalid/`, о котором должно прийти сообщение.

| Режим | Нарушения | Стиль компонента |
|---|---|---|
| упакованный AppImage (production-сборка) | только контрольное (`img-src`) | применён |
| `wails3 dev` (dev-сервер Vite) | `style-src-elem` для встроенного `<style>` и контрольное | не применён |

Вызовам привязок через среду выполнения Wails ничего сверх политики не нужно. В режиме разработки
Vite вставляет CSS компонентов встроенными элементами `<style>`, поэтому в `frontend/vite.config.ts`
есть плагин, который только для dev-сервера (`apply: "serve"`) меняет `style-src 'self';` на
`style-src 'self' 'unsafe-inline';` и завершается ошибкой, если строгой строки нет. После этого
`wails3 dev` сообщает только о контрольном нарушении, а `frontend/dist/index.html` по-прежнему
содержит `style-src 'self';`. WebSocket HMR разрешён правилом `connect-src 'self'`.

`wails3 dev` нужен дисплей, поэтому проверка шла в контейнере `ubuntu:26.04` с пакетами разработки
GTK и WebKitGTK и Xvfb; пробный код потом удалён.

## Проверка 3: пакеты и AppImage в чистом контейнере

`task build` и `task package:linux` создают `bin/smartnote`, `bin/smartnote.deb` и
`bin/smartnote-x86_64.AppImage`. `.deb` зависит от `libgtk-4-1`, `libwebkitgtk-6.0-4`,
`fonts-liberation` (шрифт интерфейса; `liberation-sans-fonts` в `.rpm`, `ttf-liberation` для Arch)
и `fonts-dejavu-core` (DejaVu Sans Mono для заметок; `dejavu-sans-mono-fonts`, `ttf-dejavu`).

`task smoke:appimage` запускает `build/linux/smoke-appimage.sh` в `ubuntu:26.04`. Если поставить
только `xvfb` и `xauth`, как планировалось сначала, AppImage не запускается:

1. `libharfbuzz.so.0: cannot open shared object file`. `ldd` по распакованному AppImage
   показывает ещё `libEGL.so.1`, `libfontconfig.so.1`, `libfribidi.so.0`; `libGLESv2.so.2`
   загружается через `dlopen` и падает позже. Эти библиотеки есть в списке исключений AppImage и
   ожидаются от системы.
2. `Failed to spawn child process "/usr/bin/bwrap"`, затем SIGABRT: WebKitGTK 6.0 запускает свой
   веб-процесс в песочнице bubblewrap, и ему нужен системный `bwrap` (и `xdg-dbus-proxy`).

В любой настольной Ubuntu 26.04 эти пакеты есть (`libwebkitgtk-6.0-4` зависит от `bubblewrap` и
`xdg-dbus-proxy`; библиотеки входят в базу рабочего стола). Это не пакеты разработки, поэтому
проверка теперь ставит их:
`xvfb xauth dbus libegl1 libgles2 libfontconfig1 libfribidi0 libharfbuzz0b bubblewrap xdg-dbus-proxy`.

bubblewrap нужны непривилегированный пользователь и пространства имён пользователей, которые Docker
по умолчанию блокирует. Контейнер запускается с `--security-opt seccomp=unconfined --security-opt
apparmor=unconfined --security-opt systempaths=unconfined`, а приложение работает от обычного
пользователя под `xvfb-run -a dbus-run-session`. От root внутри контейнера bwrap всё равно падает
(`Creating new namespace failed`).

Результат: `smoke: exit code 0`, `smoke: ok`: AppImage работает 15 секунд с запущенным
`WebKitWebProcess`, в журнале нет ошибок библиотек и падений, а SIGTERM завершает его с кодом 0.
Контрольный запуск без `bubblewrap` не проходит проверку (`application exited early`, код выхода 2).

Следствие для [руководства по установке](install.ru.md#ubuntu-appimage): AppImage поддерживает
настольные Ubuntu; минимальной системе нужны перечисленные выше пакеты.
