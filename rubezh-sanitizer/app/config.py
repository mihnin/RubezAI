"""Конфигурация сервиса rubezh-sanitizer."""

from __future__ import annotations

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Настройки сервиса. Источник — переменные окружения с префиксом SANITIZER_."""

    model_config = SettingsConfigDict(env_prefix="SANITIZER_", extra="ignore")

    app_name: str = "rubezh-sanitizer"
    port: int = 8001


settings = Settings()
