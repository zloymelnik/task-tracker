# Task Tracker

## Назначение
REST API сервис для управления задачами внутри команд с ролевой
моделью, историей изменений, кешированием списка задач и SQL отчетом

### Описание
Сейчас реализованы HTTP-сервер, /health, конфигурация, структурированные логи и корректное завершение приложения. MySQL и Redis запускаются через Compose; подключение приложения к ним и бизнес-функции запланированы.

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

# 3. Запуск
make build
make up

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

