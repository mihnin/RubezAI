# Рубеж ИИ

**Программный комплекс безопасной маршрутизации, обезличивания, аудита и
контроля запросов к системам искусственного интеллекта.**

On-prem-ready приложение для госкомпаний, операторов КИИ и enterprise. Позволяет
сотрудникам безопасно использовать LLM, а службе ИБ, юристам и администраторам —
контролировать данные, модели, политики, инциденты и аудит.

## Главный сценарий

Пользователь вводит запрос или загружает договор → система находит ПДн,
реквизиты, коммерческую тайну, секреты и рискованные фрагменты → обезличивает
данные → policy engine принимает решение → запрос уходит в разрешённую LLM →
ответ проверяется → пользователь получает результат → ИБ видит полный audit trail.

Принцип архитектуры: **Rules-first, LLM-assisted, policy-decided**.

## Архитектура

| Сервис | Стек | Назначение |
|--------|------|------------|
| `rubezh-web` | React + TypeScript | Пользовательский и админский интерфейс |
| `rubezh-api` | Go | API Gateway, auth, Policy Engine, LLM Router, Audit API |
| `rubezh-sanitizer` | Python / FastAPI | Детекция и обезличивание ПДн, секретов, коммерческих данных |
| `rubezh-worker` | Python | Парсинг документов, chunking, embeddings, индексация |
| PostgreSQL + pgvector | PostgreSQL 16 | Единый source of truth, audit, embeddings |
| MinIO | — | Object storage документов |

Подробно — [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## Документация

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — архитектура и решения
- [`docs/PLAN.md`](docs/PLAN.md) — живой план реализации по итерациям
- [`docs/API.md`](docs/API.md) — REST API
- [`docs/THREAT_MODEL.md`](docs/THREAT_MODEL.md) — модель угроз
- [`docs/SECURITY_CHECKLIST.md`](docs/SECURITY_CHECKLIST.md) — чеклист безопасности
- [`docs/contracts/`](docs/contracts/) — межсервисные контракты (JSON Schema)

## Требования

- Docker 24+ и Docker Compose v2
- Для разработки frontend — Node.js 20+
- Go SDK **не требуется** — `rubezh-api` собирается в Docker

## Быстрый старт

```bash
# 1. Подготовить переменные окружения
cp .env.example .env          # Windows: Copy-Item .env.example .env

# 2. Поднять инфраструктуру (PostgreSQL + MinIO)
docker compose up -d postgres minio
#   Linux/CI:  make infra
#   Windows:   .\make.ps1 infra

# 3. Проверить статус
docker compose ps
```

MinIO-консоль после запуска — http://localhost:9001 (логин/пароль из `.env`).

> Текущий статус — Итерация 0 (скелет репозитория). Прикладные сервисы
> (`rubezh-api`, `rubezh-sanitizer`, `rubezh-worker`, `rubezh-web`) добавляются
> в своих итерациях согласно [`docs/PLAN.md`](docs/PLAN.md).

## Статус проекта

MVP в активной разработке. Прогресс по итерациям — в [`docs/PLAN.md`](docs/PLAN.md).

## Лицензия

MIT — см. [LICENSE](LICENSE).
