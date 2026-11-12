# Task Tracker

## Назначение

REST API сервис для управления задачами внутри команд с ролевой
моделью, историей изменений, кешированием списка задач и SQL отчетом

## Возможности сервиса

- **Команды и роли.** Создание команд, список своих команд, добавление
  зарегистрированных пользователей по email и управление ролями `owner`,
  `admin`, `member`. Доступ ограничен членством в команде.
- **Задачи.** Создание, назначение исполнителя, статусы `todo`, `in_progress`,
  `done`, фильтрация и пагинация. Owner/admin могут редактировать любые задачи
  своей команды; member-автор — свою задачу, member-исполнитель — только её статус.
- **История и комментарии.** Изменения содержат старые и новые значения,
  автора и время. История и комментарии доступны участникам команды задачи.
- **Авторизация.** Регистрация с bcrypt и вход с JWT. Защищённые запросы требуют
  `Authorization: Bearer <JWT>`; срок действия токена по умолчанию — 5 минут.
- **Целостность данных.** MySQL-транзакции объединяют создание команды с членством
  владельца, а создание/обновление задачи — с историей. Поле `version` защищает
  от потери конкурентных изменений: устаревшая версия возвращает `409`.
- **Кеширование.** Redis хранит страницы списка задач вместе с `total` на 5 минут.
  Ключ учитывает команду, фильтры и пагинацию. После commit изменений кеш команды
  инвалидируется; доступ проверяется и при попадании в кеш. `singleflight`
  объединяет одновременные загрузки одной страницы внутри экземпляра приложения.
- **Статистика.** Один SQL-запрос с CTE считает задачи по статусам, топ-3
  исполнителей по закрытым задачам за 30 дней, среднее время закрытия и
  количество комментариев. Отчёт доступен только owner/admin команды.
- **Эксплуатация.** Swagger UI, конфигурация YAML/env, Request ID,
  структурированные логи, восстановление после panic и корректное завершение.

Особенности API:

- UUID используются для идентификаторов; время хранится в UTC с точностью до миллисекунд.
- Списки возвращают `items`, `total`, `limit`, `offset`; `limit` по умолчанию
  `20` (от `1` до `100`), `offset` — `0`. Пустой список — `[]`.
- Обновление задачи выполняется через `PUT` с обязательной `version`.
  Отсутствующие поля сохраняются; `assignee_id: null` снимает исполнителя.
  Запрос без изменений не увеличивает версию и не добавляет историю.
- JSON-запрос ограничен 64 КиБ. Ошибки имеют формат `{"error":"..."}`;
  внутренние диагностические сообщения не возвращаются клиенту.
- При ошибках кеша список читается из MySQL. Ошибка инвалидации не откатывает
  уже сохранённые данные; старая страница может оставаться доступной до TTL.
  Для запуска приложения подключения к MySQL и Redis обязательны.

## Быстрый старт для разработки

Нужны Git, Docker с Compose v2 и Make. Для работы в devcontainer — VS Code
с расширением **Dev Containers**. Примеры команд ниже рассчитаны на Bash/zsh.

**На хостовой машине**, в каталоге проекта:

```sh
git clone https://github.com/zloymelnik/task-tracker.git
cd task-tracker
cp .env.example .env
```

В `.env` задай собственные `DB_PASSWORD`, `DB_ROOT_PASSWORD`, `REDIS_PASSWORD`
и `JWT_SECRET`; значения из примера предназначены для настройки, а не production.
JWT-секрет можно сгенерировать командой `openssl rand -hex 32`.
Для Swagger UI установи `DOCS_ENABLED=true`.

Собери окружение, запусти сервисы и примени миграции:

```sh
make build
make up
docker compose exec app make migrate-up
curl -i http://localhost:8080/api/v1/health
```

Ожидаемый ответ health — `200` и `OK`. При изменении `APP_PORT` в `.env`
используй соответствующий порт. Dev-сервисы MySQL и Redis также публикуют порты;
при конфликте измени `HOST_DB_PORT` и `HOST_REDIS_PORT`.

Открой проект через **Dev Containers: Reopen in Container**. Инструменты Go,
Air, golangci-lint, migrate и swag уже установлены в образе разработки.
Исходники смонтированы в `/app`, Air пересобирает и перезапускает Go-приложение.
Docker-команды выполняй на хосте; `make test`, `make lint`, `make swag` и
`make migrate-up` доступны в терминале контейнера.

Остановка из терминала хоста:

```sh
make down
```

Данные MySQL и Redis сохраняются в volumes. Перезапуск или пересоздание
контейнера разработки прерывает работающие в нём терминалы и сессии инструментов.

## Быстрый старт для production

Нужны Docker с Compose v2, файл `.env` с настроенными секретами и каталог
`migrations/` из репозитория. Установка Go на хосте не требуется.

**На хостовой машине**, из корня проекта:

```sh
APP_PORT=18080 DOCS_ENABLED=false docker compose \
  -p team-tasks-prod -f compose.prod.yml config --quiet

APP_PORT=18080 DOCS_ENABLED=false docker compose \
  -p team-tasks-prod -f compose.prod.yml up -d --build

docker compose -p team-tasks-prod -f compose.prod.yml ps -a
curl -i http://localhost:18080/api/v1/health
```

Здесь `-p team-tasks-prod` создаёт отдельный Compose-проект с собственными
контейнерами, сетью и volumes. Порт `18080` позволяет проверить production рядом
с dev-приложением на `8080`. При каждом `up` сохраняй выбранные имя проекта,
порт и настройки. Присваивания перед командой в Bash действуют только на неё.

Порядок запуска:

1. MySQL и Redis проходят healthcheck.
2. Одноразовый контейнер `migrate` применяет миграции к MySQL.
3. Приложение запускается после успешных миграций и готовности зависимостей.

Для `migrate` ожидается **`Exited (0)`** — это успешное завершение, а не сбой.
При ошибке миграций приложение не запускается. Логи миграций:

```sh
docker compose -p team-tasks-prod -f compose.prod.yml logs migrate
```

Приложение собирается из стадии `prod` Dockerfile и запускается от
непривилегированного пользователя. В образе находятся бинарник и конфиг;
исходники и Air для работы приложения не нужны. MySQL и Redis не публикуют
порты на хост, данные хранятся в volumes. SQL-файлы монтируются только в
контейнер миграций, в режиме чтения. Спецификация Swagger встроена в бинарник.

При `DOCS_ENABLED=false` адреса `/swagger/index.html` и `/openapi.json`
возвращают `404`. Health проверяет доступность HTTP-сервера, а не выполняет
повторную проверку подключений к базам.

Для обновления приложения и применения новых миграций повтори `up -d --build`
с теми же параметрами. Миграции рассчитаны на первоначальный запуск и
согласованные обновления схемы; запуск нескольких версий приложения требует
учёта совместимости схемы.

Остановка с сохранением данных:

```sh
docker compose -p team-tasks-prod -f compose.prod.yml down
```

Не добавляй `-v`, если данные нужно сохранить: этот флаг удаляет volumes.

## Документация API

Все маршруты API имеют префикс `/api/v1`. Контракты запросов, ответов,
параметры фильтрации и коды ошибок описаны в Swagger:

- [Swagger UI при dev-запуске](http://localhost:8080/swagger/index.html).
- [JSON-спецификация](http://localhost:8080/openapi.json).
- Файлы в репозитории: [swagger.yaml](api/openapi/swagger.yaml) и
  [swagger.json](api/openapi/swagger.json).

Чтобы открыть UI, задай `DOCS_ENABLED=true` в `.env` и **на хосте** примени
настройку окружения:

```sh
docker compose up -d app
```

Compose при необходимости пересоздаст приложение, что прервёт активные сессии
в devcontainer. Простое изменение `.env` не меняет окружение уже запущенного
процесса. В production документация выключена по умолчанию; для включения
передай `DOCS_ENABLED=true` при `up` выбранного production-проекта.

Для авторизации выполни `POST /api/v1/login`, скопируй значение `token` и нажми
**Authorize**. В поле введи `Bearer <JWT>`. Регистрация, вход и health публичные.
**Try it out → Execute** отправляет настоящие запросы к запущенному сервису
и может создавать или изменять данные.

Генерация после изменения аннотаций или DTO — **в devcontainer**:

```sh
make swag
```

Используется Swag v1.16.4, формат спецификации — **Swagger 2.0**.
Аннотации находятся в `cmd/task-tracker/main.go` и `internal/handler/`.
Команда обновляет JSON/YAML, собирает `tmp/main` и вызывает событие записи
`api/openapi/embed.go`, чтобы работающий Air пересобрал и перезапустил приложение.
Без Air команда только обновляет файлы и бинарник; запущенный процесс нужно
перезапустить отдельно. Для production после генерации пересобери образ.

JSON встроен через `go:embed`: изменение файла без пересборки не обновляет
спецификацию работающего бинарника. Сгенерированные файлы сохраняются в Git;
редактировать нужно аннотации и теги DTO.

В статистике период 30 дней применяется только к топу исполнителей и включает
нижнюю границу по `closed_at`. Среднее считается в секундах по всем текущим
`done` с заполненным `closed_at`. При равенстве результатов топ упорядочен
по UUID исполнителя. У пустой команды счётчики нулевые, топ — `[]`, среднее — `null`.

## Техническая часть

### Стек и структура

Сервис написан на Go 1.26.11, использует MySQL 8.4 и Redis 7.
HTTP-сервер и структурированные логи построены на стандартных `net/http`
и `log/slog`.

Основные прямые зависимости и их назначение:

| Пакет | Зачем используется |
|-------|--------------------|
| `github.com/go-chi/chi/v5` | Маршрутизация HTTP и подключение middleware |
| `github.com/go-sql-driver/mysql` | Драйвер подключения к MySQL |
| `github.com/jmoiron/sqlx` | Выполнение SQL и чтение результатов в структуры |
| `github.com/redis/go-redis/v9` | Подключение к Redis и операции кеша |
| `github.com/golang-jwt/jwt/v5` | Создание и проверка JWT |
| `golang.org/x/crypto/bcrypt` | Хеширование и проверка паролей |
| `golang.org/x/sync/singleflight` | Объединение одновременных загрузок одной страницы внутри экземпляра приложения |
| `github.com/google/uuid` | Создание и разбор UUID |
| `github.com/ilyakaznacheev/cleanenv` | Чтение конфигурации из YAML и окружения |
| `github.com/avast/retry-go` | Повторные попытки подключения к MySQL и Redis при запуске |
| `github.com/swaggo/http-swagger/v2` | Отображение спецификации API через Swagger UI |

Точные версии библиотек зафиксированы в [go.mod](go.mod).
Транзитивные зависимости (`// indirect`) здесь не перечислены.

Инструменты разработки и запуска:

| Инструмент | Назначение |
|------------|------------|
| Docker Compose | Запуск приложения, MySQL, Redis и production-миграций |
| Air | Пересборка и перезапуск приложения при изменении исходников в разработке |
| golang-migrate v4.18.2 | Применение и откат SQL-миграций |
| swag v1.16.4 | Генерация Swagger-спецификации из аннотаций Go |
| golangci-lint v2.10.1 | Проверка кода линтерами |
| govulncheck | Проверка известных уязвимостей в зависимостях и используемом коде |
| GitHub Actions | Автоматический запуск проверок при push и pull request |

```text
cmd/task-tracker/      Точка входа
internal/app/          Сборка зависимостей, маршруты, жизненный цикл
internal/config/       Загрузка и проверка конфигурации
internal/domain/       Доменные модели и ошибки
internal/handler/      HTTP, DTO, middleware, Swagger-аннотации
internal/service/      Бизнес-логика и проверки прав
internal/storage/      Репозитории MySQL, транзакции и Redis
internal/logger/       Настройка логирования
configs/               YAML-конфигурация
migrations/            SQL-миграции up/down
api/openapi/           Спецификация и go:embed
tests/                 Основной набор тестов
docker/                Dockerfile
```

### Конфигурация

Основной файл — [configs/config.yaml](configs/config.yaml). Другой путь можно
передать бинарнику флагом `-config-path`. YAML задаёт значения, переменные
окружения их переопределяют. Приложение само не читает `.env`: его используют
Compose и Makefile. Не все переменные из `.env` автоматически попадают в
контейнер — они должны быть перечислены в `environment` Compose.

| Настройка | Переменные окружения | Текущее значение в YAML |
|-----------|---------------------|------------------------|
| Режим логирования | `ENV` | По умолчанию приложения `local`; production Compose задаёт `prod` |
| HTTP-порт и таймауты | `APP_PORT`, `SERVER_READ_TIMEOUT`, `SERVER_WRITE_TIMEOUT`, `SERVER_SHUTDOWN_TIMEOUT` | `8080`, `5s`, `10s`, `15s` |
| MySQL | `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER` | `mysql`, `3306`, `team_tasks`, `app` |
| MySQL: таймаут, пул, повторы | `DB_POOL_MAX_REQUEST_TIME`, `DB_POOL_MAX_OPEN_CONNS`, `DB_POOL_MAX_IDLE_CONNS`, `DB_POOL_CONN_MAX_LIFETIME`, `DB_RETRY_MAX_ATTEMPTS`, `DB_RETRY_DELAY` | `5s`, `10`, `5`, `30m`, `10`, `2s` |
| JWT | `JWT_TTL` | `5m` |
| Redis | `REDIS_ADDR`, `REDIS_DB`, `REDIS_POOL_SIZE` | `redis:6379`, `0`, `10` |
| Redis: операция, TTL, загрузка страницы | `REDIS_MAX_REQUEST_TIME`, `REDIS_CACHE_TTL`, `REDIS_LOAD_TIMEOUT` | `5s`, `5m`, `5s` |
| Redis: повторы подключения | `REDIS_RETRY_MAX_ATTEMPTS`, `REDIS_RETRY_DELAY` | `10`, `2s` |
| Документация | `DOCS_ENABLED` | `false` |

Секреты `DB_PASSWORD`, `REDIS_PASSWORD` и `JWT_SECRET` читаются только из env.
`DB_ROOT_PASSWORD` используется контейнером MySQL. `.env` исключён из Git и
контекста Docker-сборки. Шаблон настроек — [.env.example](.env.example).

Compose фиксирует внутренний порт приложения `8080`; `APP_PORT` на хосте
определяет публикуемый порт. В production YAML-конфиг скопирован в образ:
для изменения файла нужна пересборка образа. Новые env-параметры применяются
при пересоздании контейнера через `up -d`, а не простым `restart`.

### Логи

На хосте — stdout контейнера приложения:

```sh
docker compose logs -f app
docker compose -p team-tasks-prod -f compose.prod.yml logs -f app
```

В режиме `local` также ведётся текстовый файл; **в devcontainer**:

```sh
tail -f tmp/app.log
```

`ENV=dev` — JSON с уровнем DEBUG, `ENV=prod` — JSON с уровнем INFO.
HTTP-логи содержат Request ID, метод, путь, статус и длительность запроса;
идентификатор возвращается в заголовке `X-Request-Id`.
Ошибки сборки Air записываются в `tmp/build-errors.log`.

### Makefile

| Команда | Назначение | Где выполнять |
|---------|------------|----------------|
| `make help` | Список команд | Хост или devcontainer |
| `make build` | Сборка dev-образа | Хост |
| `make up` | Запуск dev Compose | Хост |
| `make down` | Остановка dev Compose с сохранением volumes | Хост |
| `make restart` | `down` и `up` dev-окружения | Хост |
| `make logs-app` | Логи dev-приложения | Хост |
| `make migrate-create NAME=...` | Новая пара SQL-файлов | Devcontainer |
| `make migrate-up` | Применение миграций | Devcontainer |
| `make migrate-down` | Откат одной миграции | Devcontainer |
| `make swag` | Генерация спецификации и сборка бинарника | Devcontainer |
| `make test` | Тесты с `-race` и покрытием | Devcontainer |
| `make lint` | golangci-lint | Devcontainer |

Docker-цели Makefile используют `compose.yml` для разработки. Production
управляется явно через `docker compose -p ... -f compose.prod.yml`.
`make restart` затрагивает devcontainer — не используй его для перезапуска
отдельного production-проекта.

### Миграции

Пары файлов `migrations/<номер>_<название>.up.sql` / `.down.sql` задают
применение и откат схемы. В devcontainer:

```sh
make migrate-create NAME=add_task_index
make migrate-up
make migrate-down
```

`make migrate-down` откатывает **одну** последнюю миграцию. Откат может удалять
таблицы и данные — сначала проверь соответствующий `.down.sql`.

В production миграции применяет сервис `migrate` перед запуском приложения.
Для явного повторного применения оставшихся миграций на хосте:

```sh
docker compose -p team-tasks-prod -f compose.prod.yml run --rm migrate
```

Когда всё применено, сообщение `no change` нормально. Не редактируй уже
применённые миграции в общем окружении: изменения схемы оформляй новой парой
файлов. Для обновлений используй `up -d --build`; `restart` не заменяет сборку
образа и не применяет изменённое окружение.

### Тесты, покрытие и CI

Основные тесты находятся в `tests/`, повторяя структуру `internal/`.
Тесты статистики расположены рядом с реализацией в `internal/app`,
`internal/handler`, `internal/service` и `internal/storage/mysql`.
Команда `go test ./...` запускает обе группы.

**В devcontainer**, полный прогон с MySQL и Redis:

```sh
MYSQL_TEST_DSN="${DB_USER}:${DB_PASSWORD}@tcp(mysql:3306)/${DB_NAME}" \
REDIS_TEST_ADDR=redis:6379 \
REDIS_TEST_PASSWORD="$REDIS_PASSWORD" \
  make test

make lint
go build ./...
```

Интеграционные тесты MySQL требуют `MYSQL_TEST_DSN`, Redis — `REDIS_TEST_ADDR`
и, при включённой аутентификации, `REDIS_TEST_PASSWORD`. Можно задать
`REDIS_TEST_DB` (по умолчанию `0`). Без настроек соответствующие тесты
отмечаются `SKIP`, а функции, проверяемые только ими, могут показывать `0%`
покрытия. Это не означает отсутствия тестов.

MySQL-тесты используют временные таблицы либо обычные таблицы со случайным
префиксом, реальные миграции и внешние ключи. Redis-тесты создают уникальные
ключи. Тестовые данные очищаются; существующие записи приложения не меняются.
Для MySQL нужны права создания и удаления таблиц, root не требуется.

Только SQL-отчёт или один его сценарий:

```sh
MYSQL_TEST_DSN="${DB_USER}:${DB_PASSWORD}@tcp(mysql:3306)/${DB_NAME}" \
  go test -v -race -count=1 -run '^TestTeamStats$' ./internal/storage/mysql

MYSQL_TEST_DSN="${DB_USER}:${DB_PASSWORD}@tcp(mysql:3306)/${DB_NAME}" \
  go test -v -race -count=1 -run '^TestTeamStats/top_ties$' ./internal/storage/mysql
```

`make test` создаёт `coverage.out` с покрытием пакетов `internal/`, в том числе
тестами из отдельных каталогов. Просмотр по функциям и общий результат `total`:

```sh
go tool cover -func=coverage.out
go tool cover -html=coverage.out -o /tmp/task-tracker-coverage.html
```

Для просмотра покрытия строк открой полученный HTML-файл в браузере.

Проверка известных уязвимостей:

```sh
go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
govulncheck ./...
```

GitHub Actions проверяет форматирование, сборку, тесты с `-race` и покрытием,
линтер, сборку production-образа и уязвимости. Отчёт покрытия сохраняется
артефактом. Сейчас CI поднимает MySQL; Redis-интеграции запускаются отдельно
локально, поскольку Redis-сервис в workflow пока не настроен.

## Дальнейшие улучшения

- Ограничение частоты запросов для регистрации и входа.
- Circuit breaker для Redis с восстановлением кеша после пропущенных инвалидаций.
- Запуск Redis-интеграций в CI.

