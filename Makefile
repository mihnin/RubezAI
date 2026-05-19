# Рубеж ИИ — Makefile (Linux / CI).
# Windows без make: используйте make.ps1 — те же цели.

COMPOSE := docker compose

.PHONY: help infra infra-down config ps logs clean

help:           ## Показать список целей
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-14s %s\n", $$1, $$2}'

infra:          ## Поднять инфраструктуру (PostgreSQL + MinIO)
	$(COMPOSE) up -d postgres minio

infra-down:     ## Остановить инфраструктуру
	$(COMPOSE) down

config:         ## Проверить конфигурацию compose
	$(COMPOSE) config

ps:             ## Статус сервисов
	$(COMPOSE) ps

logs:           ## Логи сервисов (Ctrl+C для выхода)
	$(COMPOSE) logs -f

clean:          ## Остановить и удалить тома — ВНИМАНИЕ: удаляет данные
	$(COMPOSE) down -v
