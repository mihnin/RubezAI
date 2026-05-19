# Рубеж ИИ — Makefile (Linux / CI).
# Windows без make: используйте make.ps1 — те же цели.

COMPOSE := docker compose

.PHONY: help infra infra-down config migrate migrate-verify ps logs clean

help:           ## Показать список целей
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-14s %s\n", $$1, $$2}'

infra:          ## Поднять инфраструктуру (PostgreSQL + MinIO)
	$(COMPOSE) up -d postgres minio

infra-down:     ## Остановить инфраструктуру
	$(COMPOSE) down

config:         ## Проверить конфигурацию compose
	$(COMPOSE) config

migrate:        ## Применить миграции БД
	$(COMPOSE) run --rm migrate

migrate-verify: migrate  ## Применить миграции и проверить схему БД
	$(COMPOSE) exec -T postgres psql -U $${POSTGRES_USER:-rubezh} -d $${POSTGRES_DB:-rubezh} -v ON_ERROR_STOP=1 -f - < rubezh-api/migrations/tests/verify_schema.sql

ps:             ## Статус сервисов
	$(COMPOSE) ps

logs:           ## Логи сервисов (Ctrl+C для выхода)
	$(COMPOSE) logs -f

clean:          ## Остановить и удалить тома — ВНИМАНИЕ: удаляет данные
	$(COMPOSE) down -v
