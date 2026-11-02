# Task Tracker

## Назначение
REST API сервис для управления задачами внутри команд с ролевой
моделью, историей изменений, кешированием списка задач и SQL отчетом

### Описание
Сейчас реализованы:
- HTTP-сервер с проверкой состояния GET /health.
- Регистрация пользователей с сохранением в MySQL и хешированием паролей bcrypt.
- Вход с выдачей JWT, проверка токенов через middleware.
- Валидация запросов и единый JSON-формат ошибок.
- Request ID, логирование HTTP-запросов и восстановление после panic.
- Конфигурация через YAML и переменные окружения, корректное завершение приложения.

Команды, роли, задачи, история, комментарии, Redis-кеш и SQL-отчёт
запланированы. Redis запускается через Compose, но приложение пока его не использует.

## Стек
| Технология | Назначение |
|------------|------------|
| Go | Язык |
| MySQL | База данных |
| Redis | Кеш |
| Swagger | Документация API |

## Требования
Docker с Compose и Make. Для devcontainer — VS Code с расширением Dev Containers.

## Быстрый старт
```bash
# 1. Клонировать
git clone https://github.com/zloymelnik/task-tracker.git
cd task-tracker

# 2. Переменные окружения
cp .env.example .env
# Перед запуском замени JWT_SECRET в .env на случайный секрет.
# Например, сгенерируй его командой:

#     openssl rand -hex 32

# Секрет читается только из окружения. Срок действия токена задаётся
# в configs/config.yaml через auth.jwt_ttl; текущее значение — 5m.

# Приложение также поддерживает переменную JWT_TTL. Для использования
# через Compose её нужно дополнительно передать в environment сервиса app.

# 3. Запуск
make build
make up
docker compose exec app make migrate-up

# 4. Проверка
curl http://localhost:8080/health
# Ожидаемый ответ: OK
# Если изменён APP_PORT в .env, используй соответствующий порт

# 5. Тесты и линтер внутри devcontainer
# Открыть проект в VS Code → Dev Containers: Reopen in Container → выполнять команды в его терминале
make test
make lint

# 6. Остановка
make down
```

## Авторизация

Регистрация:

```bash
curl -i http://localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","name":"User","password":"example-password"}'
```

Успешный ответ: 201 с полями id, email, name, created_at.
Повторный email возвращает 409.

Вход:

```bash
curl -i http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","password":"example-password"}'
```

Успешный ответ: 200 с телом `{"token":"<JWT>"}`.
Неверные учётные данные возвращают 401.

Для защищённых маршрутов токен передаётся в заголовке:

    Authorization: Bearer <JWT>

Защищённые бизнес-маршруты будут добавлены на этапе команд и задач.

Ошибки возвращаются в формате `{"error":"..."}`.
Некорректный запрос возвращает 400, превышение лимита JSON в 64 КиБ — 413,
внутренняя ошибка — 500.

## Проверки

В терминале devcontainer:

```bash
make test
make lint
go build ./...
```

MySQL-интеграции запускаются при заданном MYSQL_TEST_DSN.
Без него они пропускаются. Для подключения к MySQL из devcontainer:

```bash
MYSQL_TEST_DSN="${DB_USER}:${DB_PASSWORD}@tcp(mysql:3306)/${DB_NAME}" \
  go test -race -count=1 -v ./internal/storage/mysql
```

Интеграционные тесты используют временные таблицы и не изменяют
существующие записи.

## Логи приложения в devcontainer

При `ENV=local` приложение пишет логи в stdout и `tmp/app.log`.
Для просмотра в терминале VS Code:

```bash
tail -f tmp/app.log
```

`tmp/build-errors.log` содержит ошибки сборки Air.

