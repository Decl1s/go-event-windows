# Мой фактический стек

29.10.2026 · версия 0.1.0. Я выбрал Go для обработки событий, а Python — только
для инструментов разработки. У CLI нет сетевых вызовов, БД или сторонних пакетов.

| Технология | Закрепление / наблюдение | Для чего я использую и почему | Официальный источник |
| --- | --- | --- | --- |
| Go | 1.27.3, go.mod и GOTOOLCHAIN в CI | Я получаю типизированный CLI и актуальный security patch | [Release history](https://go.dev/doc/devel/release), [download](https://go.dev/dl/) |
| Go standard library | Поставляется с Go1.27.3 | Я использую bufio/io для JSONL, encoding/json для строгого разбора токенов, time для UTC окон, regexp/sort для контракта и порядка, flag/os/fmt/errors для CLI; bytes/strings/reflect/testing — для проверок | [Packages](https://pkg.go.dev/std), [bufio](https://pkg.go.dev/bufio), [json](https://pkg.go.dev/encoding/json), [time](https://pkg.go.dev/time) |
| Go tools | 1.27.3 | Я выполняю gofmt, go vet, go test, race detector на Linux и go build -trimpath | [go command](https://pkg.go.dev/cmd/go), [race](https://go.dev/doc/articles/race_detector), [toolchains](https://go.dev/doc/toolchain) |
| Python | 3.14.4, CI и локальная проверка | Я объединяю CLI gate, упаковку и GitHub release без pip | [3.14 docs](https://docs.python.org/3.14/) |
| Python standard library | Вместе с Python3.14.4 | Я использую pathlib/tempfile/subprocess/argparse для запуска, json/re/hashlib/zipfile для контроля ZIP и urllib/os для guarded release; ZIP_STORED не требует zlib | [zipfile](https://docs.python.org/3.14/library/zipfile.html), [urllib](https://docs.python.org/3.14/library/urllib.request.html) |
| Git | Локально 2.53.0 | Я проверяю отдельный корень, явный список файлов и неизменность HEAD; версия Git в hosted runner управляется образом | [git](https://git-scm.com/docs) |
| GitHub Actions | ubuntu-24.04 / windows-2025, образы обновляются GitHub | Я проверяю две ОС, release зависит от обоих check; runtime Actions управляется GitHub | [workflow syntax](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax) |
| actions/checkout | 7.0.1, SHA в DEPENDENCIES | Я читаю исходники без сохранённых Git credentials | [checkout](https://github.com/actions/checkout) |
| actions/setup-go | 7.0.0, SHA в DEPENDENCIES | Я устанавливаю строго Go1.27.3 без module cache | [setup-go](https://github.com/actions/setup-go) |
| actions/setup-python | 7.1.0, SHA в DEPENDENCIES | Я устанавливаю Python3.14.4 | [setup-python](https://github.com/actions/setup-python) |
| JSON / JSONL, Markdown, ZIP, SHA-256 | Контракт schema_version1; docs; ZIP STORE | Я сохраняю читаемые результаты, объяснения и проверяемый архив | [JSON RFC8259](https://www.rfc-editor.org/rfc/rfc8259), [hashlib](https://docs.python.org/3.14/library/hashlib.html) |
| VS Code tasks | tasks schema2.0.0, редактор необязателен | Я даю три запуска без дополнительных расширений | [tasks](https://code.visualstudio.com/docs/debugtest/tasks) |

## Как я фиксирую полное дерево

В `DEPENDENCIES.json` я перечислил Go/Python и SHA всех Actions. Внешних прямых и
транзитивных Go-модулей **ноль**: `go list -m all` возвращает только мой модуль.
Поэтому go.sum отсутствует осознанно; go.mod закрепляет минимальную версию toolchain,
а GOTOOLCHAIN в CI выбирает точную. Python-пакетов тоже ноль. Стандартные библиотеки
я получаю вместе с указанными дистрибутивами. Зависимости самих Actions я не
подключаю к приложению: я фиксирую их готовые vendor bundles SHA коммита.
Образ hosted runner целиком я не считаю неизменяемым lock-файлом.
