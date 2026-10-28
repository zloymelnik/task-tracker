include .env
export

.PHONY: help up down restart logs-app build test lint migrate-create migrate-up migrate-down swag

help: ## Показать список доступных команд
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}'

up: ## Запустить все сервисы (app, mysql, redis)
	docker compose up -d

down: ## Остановить все сервисы
	docker compose down

restart: down up ## Перезапустить контейнеры

logs-app: ## Логи сервиса приложения
	docker compose logs -f app

build: ## Собрать образ приложения
	docker compose build app

test: ## Запустить unit и интеграционные тесты
	go test -v -race -coverprofile=coverage.out ./...

lint: ## Запустить golangci-lint
	golangci-lint run ./...

migrate-create: ## Создать новую пару миграций (NAME=create_users_table)
	migrate create -ext sql -dir migrations -seq $(NAME)

migrate-up: ## Применить все миграции
	migrate -path migrations -database "mysql://$(DB_USER):$(DB_PASSWORD)@tcp($(DB_HOST):$(DB_PORT))/$(DB_NAME)" up

migrate-down: ## Откатить последнюю миграцию
	migrate -path migrations -database "mysql://$(DB_USER):$(DB_PASSWORD)@tcp($(DB_HOST):$(DB_PORT))/$(DB_NAME)" down 1

swag: ## Сгенерировать документацию Swagger/OpenAPI
	swag init -g cmd/api/main.go -o api/openapi
