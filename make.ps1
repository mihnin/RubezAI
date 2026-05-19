# Рубеж ИИ — обёртка make для Windows (PowerShell).
# Использование:  .\make.ps1 <цель>     Цели зеркалят Makefile.

param([Parameter(Position = 0)][string]$Target = "help")

switch ($Target) {
    "infra"      { docker compose up -d postgres minio }
    "infra-down" { docker compose down }
    "config"     { docker compose config }
    "migrate"    { docker compose run --rm migrate }
    "migrate-verify" {
        docker compose run --rm migrate
        Get-Content rubezh-api/migrations/tests/verify_schema.sql -Raw |
            docker compose exec -T postgres psql -U rubezh -d rubezh -v ON_ERROR_STOP=1 -f -
    }
    "ps"         { docker compose ps }
    "logs"       { docker compose logs -f }
    "clean"      { docker compose down -v }
    default {
        Write-Host "Рубеж ИИ — доступные цели:"
        Write-Host "  infra        Поднять инфраструктуру (PostgreSQL + MinIO)"
        Write-Host "  infra-down   Остановить инфраструктуру"
        Write-Host "  config       Проверить конфигурацию compose"
        Write-Host "  migrate         Применить миграции БД"
        Write-Host "  migrate-verify  Применить миграции и проверить схему БД"
        Write-Host "  ps           Статус сервисов"
        Write-Host "  logs         Логи сервисов"
        Write-Host "  clean        Остановить и удалить тома (удаляет данные)"
    }
}
