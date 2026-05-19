"""Конфигурация сервиса rubezh-sanitizer."""

from __future__ import annotations

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Настройки сервиса. Источник — переменные окружения.

    Большинство настроек — с префиксом SANITIZER_. Ключ шифрования mapping'ов
    общий для всего комплекса и читается как MAPPING_ENCRYPTION_KEY.
    """

    model_config = SettingsConfigDict(env_prefix="SANITIZER_", extra="ignore")

    app_name: str = "rubezh-sanitizer"
    port: int = 8001
    mapping_encryption_key: str = Field(
        default="", validation_alias="MAPPING_ENCRYPTION_KEY"
    )


settings = Settings()
