# Task Tracker

## Назначение
REST API сервис для управления задачами внутри команд с ролевой
моделью, историей изменений, кешированием списка задач и SQL отчетом

### Описание
Сейчас реализованы:

- HTTP-сервер с проверкой состояния GET /api/v1/health.
- Регистрация пользователей с сохранением в MySQL и хешированием паролей bcrypt.
- Вход с выдачей JWT, проверка токенов через middleware.
- Создание команд вместе с владельцем в одной транзакции, список своих команд.
- Добавление зарегистрированных пользователей в команду по email с проверкой роли инициатора.
- Смена ролей участников владельцем команды с защитой роли owner.
- Валидация запросов и единый JSON-формат ошибок.
- Request ID, логирование HTTP-запросов и восстановление после panic.
- Конфигурация через YAML и переменные окружения, корректное завершение приложения.

Задачи, история, комментарии, Redis-кеш и SQL-отчёт
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
curl http://localhost:8080/api/v1/health
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
curl -i http://localhost:8080/api/v1/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","name":"User","password":"example-password"}'
```

Успешный ответ: 201 с полями id, email, name, created_at.
Повторный email возвращает 409.

Вход:

```bash
curl -i http://localhost:8080/api/v1/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","password":"example-password"}'
```

Успешный ответ: 200 с телом `{"token":"<JWT>"}`.
Неверные учётные данные возвращают 401.

Для защищённых маршрутов токен передаётся в заголовке:

    Authorization: Bearer <JWT>

Маршруты команд требуют этот заголовок. Регистрация, вход и health доступны без токена.

Ошибки возвращаются в формате `{"error":"..."}`.
Некорректный запрос возвращает 400, превышение лимита JSON в 64 КиБ — 413,
внутренняя ошибка — 500.

## Команды

В примерах TOKEN — JWT из ответа входа, TEAM_ID — UUID созданной команды.

```bash
curl -i http://localhost:8080/api/v1/teams \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Моя команда"}'

curl -i http://localhost:8080/api/v1/teams \
  -H "Authorization: Bearer ${TOKEN}"

curl -i "http://localhost:8080/api/v1/teams/${TEAM_ID}/invite" \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"email":"member@example.com","role":"member"}'
```

Создание возвращает 201 с полями id, name, created_at, created_by.
Создатель автоматически становится владельцем. Список возвращает 200 с массивом
команд текущего пользователя; при отсутствии команд — [].

Приглашать могут owner и admin своей команды. Допустимые роли нового участника —
admin и member; назначить owner через приглашение нельзя. Пользователь должен
быть зарегистрирован. Успешный ответ — 201 с team_id, user_id, role, joined_at.
Отсутствующий или недействительный токен возвращает 401, недостаточные права — 403,
неизвестный email — 404, повторное членство — 409. Несуществующая команда также
возвращает 403, поскольку инициатор не имеет в ней членства.

### Смена роли участника

Менять роли может только владелец команды. Допустимые значения —
`admin` и `member`. Изменять роль владельца и назначать нового `owner`
через этот endpoint нельзя. Повторное назначение текущей роли успешно.

`TOKEN` — JWT владельца, `TEAM_ID` — UUID команды,
`USER_ID` — UUID участника, которому меняют роль.

```bash
curl -i -X PATCH \
  "http://localhost:8080/api/v1/teams/${TEAM_ID}/members/${USER_ID}/role" \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"role":"admin"}'
```

Успешный ответ — 204 без тела.

| Код | Причина |
|-----|---------|
| 400 | Некорректный UUID, JSON или недопустимая роль |
| 401 | Отсутствующий или недействительный токен |
| 403 | Инициатор не владелец либо изменяется роль владельца |
| 404 | Целевой участник не найден в команде |
| 413 | Превышен лимит JSON в 64 КиБ |
| 500 | Внутренняя ошибка |

Маршрут добавлен для реализации требования управления ролями;
отдельный endpoint смены роли в исходном ТЗ не указан.

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

Большинство интеграционных тестов использует временные таблицы. Тест конкурентной
смены роли создаёт отдельные таблицы со случайным префиксом в тестовой БД и удаляет
их после проверки. Нужны права CREATE TABLE и DROP TABLE. Существующие записи
приложения не изменяются; root-доступ для тестов не нужен.

## Логи приложения в devcontainer

При `ENV=local` приложение пишет логи в stdout и `tmp/app.log`.
Для просмотра в терминале VS Code:

```bash
tail -f tmp/app.log
```

`tmp/build-errors.log` содержит ошибки сборки Air.

