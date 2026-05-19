# Рубеж ИИ — обёртка make для Windows (PowerShell).
# Использование:  .\make.ps1 <цель>     Цели зеркалят Makefile.

param([Parameter(Position = 0)][string]$Target = "help")

switch ($Target) {
    "infra"      { docker compose up -d postgres minio }
    "infra-down" { docker compose down }
    "config"     { docker compose config }
    "ps"         { docker compose ps }
    "logs"       { docker compose logs -f }
    "clean"      { docker compose down -v }
    default {
        Write-Host "Рубеж ИИ — доступные цели:"
        Write-Host "  infra        Поднять инфраструктуру (PostgreSQL + MinIO)"
        Write-Host "  infra-down   Остановить инфраструктуру"
        Write-Host "  config       Проверить конфигурацию compose"
        Write-Host "  ps           Статус сервисов"
        Write-Host "  logs         Логи сервисов"
        Write-Host "  clean        Остановить и удалить тома (удаляет данные)"
    }
}
